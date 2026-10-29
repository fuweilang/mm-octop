package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed assets/*
var assets embed.FS

const trayDoubleClick = 400 * time.Millisecond

// App is the Wails service bound to the shell UI.
type App struct {
	app            *application.App
	window         *application.WebviewWindow
	settingsWindow *application.WebviewWindow
	store          *settingsStore
	sleep          *sleepGuard
	cmd            *exec.Cmd
	mu             sync.Mutex
	quitting       bool
	updating       bool // MAMMOTH-PATCH: portable self-update in flight
	dashboardURL   string

	trayClickMu    sync.Mutex
	lastTrayClick  time.Time
	trayClickTimer *time.Timer
	drops          dropServer
	ownsListenPort bool
	// alternatePort is set only by RetryWithAlternatePort (explicit user action
	// after a port conflict). Normal boots always try the configured port.
	alternatePort bool
	// portBlocked mirrors the last reportPortOccupied so the loading page can
	// re-show the "another port" button and its message after a reload
	// (the event is fire-and-forget).
	portBlocked    bool
	portBlockedMsg string
	legacyApp      string
}

func (a *App) ServiceName() string { return "desktop" }

func (a *App) ServiceStartup(context.Context, application.ServiceOptions) error { return nil }

func (a *App) ServiceShutdown() error {
	a.sleep.stop()
	a.mu.Lock()
	cmd := a.cmd
	a.cmd = nil
	ownsPort := a.ownsListenPort
	a.ownsListenPort = false
	a.mu.Unlock()
	stopOctop(cmd)
	if ownsPort {
		clearDesktopPort()
	}
	return nil
}

func (a *App) GetSettings() Settings {
	return a.store.get()
}

func (a *App) GetSettingsStatus() (Settings, error) {
	s := a.store.get()
	if a.app == nil {
		return s, nil
	}
	status, err := a.app.Autostart.Status()
	if err != nil {
		return s, fmt.Errorf("read autostart status: %w", err)
	}
	if s.Autostart != status.Enabled {
		s.Autostart = status.Enabled
		if err := a.store.save(s); err != nil {
			return s, err
		}
	}
	return s, nil
}

func (a *App) SaveSettings(next Settings) (Settings, error) {
	cur, err := a.GetSettingsStatus()
	if err != nil {
		return cur, err
	}
	autostart, err := a.setAutostart(next.Autostart)
	if err != nil {
		return cur, err
	}
	if err := a.sleep.set(next.PreventSleep); err != nil {
		if _, rollbackErr := a.setAutostart(cur.Autostart); rollbackErr != nil {
			log.Printf("rollback autostart after sleep prevention failure: %v", rollbackErr)
		}
		return cur, err
	}
	next.Autostart = autostart
	if err := a.store.save(next); err != nil {
		return cur, err
	}
	saved := a.store.get()
	a.applyDashboardPrefs(saved)
	return saved, nil
}

func (a *App) ShowMain() {
	a.showWindow()
}

func (a *App) HideSettings() {
	if a.settingsWindow == nil {
		return
	}
	a.settingsWindow.Hide()
}

func (a *App) Quit() {
	a.requestQuit()
}

func (a *App) setAutostart(on bool) (bool, error) {
	if a.app == nil {
		return false, fmt.Errorf("autostart is unavailable before the application starts")
	}
	if on {
		if err := a.app.Autostart.Enable(); err != nil {
			return false, fmt.Errorf("enable autostart: %w", err)
		}
	} else if err := a.app.Autostart.Disable(); err != nil {
		return false, fmt.Errorf("disable autostart: %w", err)
	}
	status, err := a.app.Autostart.Status()
	if err != nil {
		return false, fmt.Errorf("read autostart status: %w", err)
	}
	if status.Enabled != on {
		return status.Enabled, fmt.Errorf("autostart state did not update")
	}
	return status.Enabled, nil
}

func (a *App) applyDashboardPrefs(s Settings) {
	if a.window == nil {
		return
	}
	js := fmt.Sprintf(
		`(function(){try{localStorage.setItem('octop:ui-locale',%s);}catch(e){}})();`,
		jsonString(string(s.Locale)),
	)
	a.window.ExecJS(js)
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// logLeftoverInstance records that a second instance of ours survived the
// cleanup. The alternate port stays remembered in that case so the leftover is
// still looked at on the next boot.
func logLeftoverInstance() {
	log.Printf("desktop: a leftover instance of ours is still up; keeping the alternate port remembered")
}

func (a *App) setStatus(msg string) {
	if a.app == nil {
		return
	}
	a.app.Event.Emit("desktop:status", msg)
}

func (a *App) boot() {
	locale := LocaleEN
	if a.store != nil {
		locale = a.store.get().Locale
	}
	recordBoot("shell_start", "", nil)
	if url := os.Getenv("OCTOP_DESKTOP_URL"); url != "" {
		a.setStatus(desktopText(locale, copyStatusConnecting))
		if err := waitHealth(locale, url, 3*time.Minute); err != nil {
			a.setStatus(err.Error())
			return
		}
		a.showDashboard(url)
		return
	}
	// Legacy 「猛犸AI」 lives on another goroutine: Spotlight/registry lookups and
	// the removal prompt must never delay startup.
	go a.promptLegacyApp()
	a.setStatus(desktopText(locale, copyStatusCheckingRuntime))
	replaced, err := ensurePortable(locale, a.setStatus)
	if err != nil {
		a.setStatus(err.Error())
		return
	}
	root := portableDir()
	a.mu.Lock()
	stopOctop(a.cmd)
	a.cmd = nil
	a.mu.Unlock()
	host, configured := resolveListen()
	preferred := a.preferredPort(host, configured)
	// Probe every port we may already own, not just `preferred`: looking only
	// at it lets a remembered (or busy) alternate hide an instance already
	// running elsewhere, and two Octop processes would share one ~/.octop
	// SQLite.
	candidates := a.candidatePorts(preferred, configured)
	existing := ""
	for _, candidate := range candidates {
		if found := findRunningDashboard(host, candidate); found != "" {
			existing = found
			break
		}
	}
	// keepAlternate stays true only while every other instance of ours is gone;
	// otherwise the alternate port must stay remembered or a leftover would
	// never be looked at again.
	keepAlternate := true
	if replaced {
		// Runtime files on disk just changed; never attach to a stale process
		// that still serves the previous code from memory.
		if !stopOurInstances(host, candidates, 0, root) {
			logLeftoverInstance()
			keepAlternate = false
		}
	} else if existing != "" {
		runningPort, hasPort := portFromDashboardURL(existing)
		// 用「本应运行的版本」判断，避免换包失败时把旧 runtime 当成当前版本。
		want := desiredRuntimeVersion(root)
		if dashboardIsCurrent(existing, want) {
			a.setStatus(desktopText(locale, copyStatusUsingExisting))
			if err := waitHealth(locale, existing, 30*time.Second); err != nil {
				a.setStatus(err.Error())
				return
			}
			// Only one instance may serve ~/.octop; drop an instance of ours on
			// another candidate. A service that merely happens to be an Octop
			// (docker proxy, dev venv) is left alone.
			if hasPort && !stopOurInstances(host, candidates, runningPort, root) {
				logLeftoverInstance()
				keepAlternate = false
			}
			if hasPort && keepAlternate {
				a.rememberListenPort(runningPort, configured)
			}
			recordBoot("attach_existing", "", nil)
			go uploadBootLog(existing)
			a.showDashboard(existing)
			return
		}
		// Right port, wrong runtime: a 1.0.x process from an older shell, or a
		// server that started before the portable was swapped. Reusing it keeps
		// serving already-fixed bugs under a new UI, so restart it.
		log.Printf(
			"desktop: stale runtime on %s (running %q, want %q); restarting",
			existing,
			dashboardVersion(existing),
			want,
		)
		a.setStatus(desktopText(locale, copyStatusReplacingStale))
		if hasPort {
			stopListeningOctop(host, runningPort)
			if !stopOurInstances(host, candidates, runningPort, root) {
				logLeftoverInstance()
				keepAlternate = false
			}
			// Restart on the same port so the origin (and the dashboard login)
			// survives the swap.
			preferred = runningPort
		}
	} else {
		// No healthy instance anywhere: whatever still holds these ports is a
		// half-started leftover of ours. Clear it now so a stale python is not
		// mistaken for a third-party program below. Only processes started from
		// this portable tree are stopped.
		if !stopOurInstances(host, candidates, 0, root) {
			logLeftoverInstance()
			keepAlternate = false
		}
	}
	a.mu.Lock()
	allowAlternate := a.alternatePort
	a.mu.Unlock()
	port, err := chooseStartPort(host, preferred, allowAlternate)
	if err != nil {
		var occupied portOccupiedError
		if errors.As(err, &occupied) {
			// Before blaming another program, try to clear a leftover of our
			// own: a crashed or half-started instance never answers
			// /api/health, so it looks like a foreign program. Only a process
			// started from this portable tree is touched.
			stopListeningOctop(host, occupied.Port)
			port, err = chooseStartPort(host, preferred, allowAlternate)
		}
	}
	if err != nil {
		var occupied portOccupiedError
		var busy noFreePortError
		var bind cannotBindError
		switch {
		case errors.As(err, &occupied):
			a.reportPortOccupied(locale, occupied.Port)
		case errors.As(err, &busy):
			a.setStatus(desktopText(locale, copyErrorNoFreePort, busy.From, busy.To))
			recordBoot("no_free_port", err.Error(), nil)
		case errors.As(err, &bind):
			a.setStatus(desktopText(locale, copyErrorCannotBind, bind.Host, bind.Port, bind.Err.Error()))
			recordBoot("cannot_bind", err.Error(), nil)
		default:
			a.setStatus(err.Error())
			recordBoot("start_failed", err.Error(), nil)
		}
		// Release the one-shot flag so the button keeps working after a retry
		// that also failed.
		a.mu.Lock()
		a.alternatePort = false
		a.mu.Unlock()
		return
	}
	a.mu.Lock()
	a.portBlocked = false
	a.portBlockedMsg = ""
	a.alternatePort = false
	cmd, err := startOctop(root, port)
	a.cmd = cmd
	a.mu.Unlock()
	if err != nil {
		a.setStatus(err.Error())
		recordBoot("service_start_failed", err.Error(), nil)
		return
	}
	base := dashboardURL(host, port)
	a.setStatus(desktopText(locale, copyStatusStartingService))
	if err := waitHealth(locale, base, 2*time.Minute); err != nil {
		a.setStatus(err.Error())
		recordBoot("health_timeout", err.Error(), nil)
		return
	}
	if err := writeDesktopPort(port); err != nil {
		log.Printf("desktop port: %v", err)
	} else {
		a.mu.Lock()
		a.ownsListenPort = true
		a.mu.Unlock()
	}
	if keepAlternate {
		a.rememberListenPort(port, configured)
	}
	recordBoot("ready", "", nil)
	go uploadBootLog(base)
	a.showDashboard(base)
}

// candidatePorts lists the ports an instance of ours may already be on: the
// port we would use, the configured one, the alternate the user accepted, and
// the port recorded for the running instance.
func (a *App) candidatePorts(preferred, configured int) []int {
	ports := make([]int, 0, 4)
	add := func(port int) {
		if port < 1 || port > 65535 {
			return
		}
		for _, existing := range ports {
			if existing == port {
				return
			}
		}
		ports = append(ports, port)
	}
	add(preferred)
	add(configured)
	if a.store != nil {
		add(a.store.get().AlternatePort)
	}
	if saved, ok := readDesktopPort(); ok {
		add(saved)
	}
	return ports
}

// preferredPort is the port to try first. Once the user accepted a port change
// we stay on it while it is free: switching back would change the origin and
// sign them out a second time.
func (a *App) preferredPort(host string, configured int) int {
	if a.store == nil {
		return configured
	}
	saved := a.store.get().AlternatePort
	if saved <= 0 || saved == configured {
		return configured
	}
	if inUse, err := listenProbe(host, saved); err == nil && !inUse {
		return saved
	}
	return configured
}

// rememberListenPort persists the port we actually started on when it differs
// from config.json, so the next launch reuses the same origin. Returning to the
// configured port clears the override.
func (a *App) rememberListenPort(port, configured int) {
	if a.store == nil {
		return
	}
	wanted := 0
	if port != configured {
		wanted = port
	}
	s := a.store.get()
	if s.AlternatePort == wanted {
		return
	}
	s.AlternatePort = wanted
	if err := a.store.save(s); err != nil {
		log.Printf("desktop: remember listen port %d: %v", port, err)
	}
}

// reportPortOccupied tells the user that another program owns the port and
// surfaces the explicit "start on another port" action. It never moves ports
// on its own: a different port is a different origin and signs the user out.
func (a *App) reportPortOccupied(locale Locale, port int) {
	msg := desktopText(locale, copyErrorPortOccupied, port)
	log.Printf("desktop: %s", msg)
	recordBoot("port_occupied", msg, nil)
	a.mu.Lock()
	a.portBlocked = true
	a.portBlockedMsg = msg
	a.mu.Unlock()
	a.setStatus(msg)
	if a.app != nil {
		a.app.Event.Emit("desktop:port-blocked", msg)
	}
}

// IsPortBlocked reports whether the last boot stopped on a port conflict.
func (a *App) IsPortBlocked() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.portBlocked
}

// PortBlockedMessage is the localized reason shown next to the "start on
// another port" button; "" when there is no conflict.
func (a *App) PortBlockedMessage() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.portBlockedMsg
}

// RetryWithAlternatePort starts the service on the next free port. Only
// reachable from the button shown after reportPortOccupied.
func (a *App) RetryWithAlternatePort() bool {
	a.mu.Lock()
	if a.alternatePort {
		a.mu.Unlock()
		return false
	}
	a.alternatePort = true
	a.mu.Unlock()
	go a.boot()
	return true
}

// legacyInstall is the path detected during boot (async, see promptLegacyApp).
func (a *App) legacyInstall() string { return a.legacyAppPath() }

func (a *App) showDashboard(base string) {
	if a.window == nil {
		return
	}
	a.dashboardURL = strings.TrimRight(base, "/") + "/"
	a.window.SetURL(base)
	a.scheduleDragOverlay()
	a.installLocalHomeGuard()
	s := a.store.get()
	go func() {
		time.Sleep(800 * time.Millisecond)
		a.applyDashboardPrefs(s)
	}()
	a.setStatus(desktopText(s.Locale, copyStatusReady))
}

func (a *App) hideToTray() {
	if a.window == nil {
		return
	}
	a.window.Hide()
}

func (a *App) showWindow() {
	if a.window == nil {
		return
	}
	if a.window.IsMinimised() {
		a.window.UnMinimise()
	}
	a.window.Show()
	a.window.Focus()
}

func (a *App) toggleMainWindow() {
	if a.window == nil {
		return
	}
	if a.window.IsVisible() && !a.window.IsMinimised() {
		a.hideToTray()
		return
	}
	a.showWindow()
}

func (a *App) onTrayLeftClick() {
	a.trayClickMu.Lock()
	defer a.trayClickMu.Unlock()
	if a.trayClickTimer != nil {
		a.trayClickTimer.Stop()
		a.trayClickTimer = nil
	}
	now := time.Now()
	if !a.lastTrayClick.IsZero() && now.Sub(a.lastTrayClick) < trayDoubleClick {
		a.lastTrayClick = time.Time{}
		go a.toggleMainWindow()
		return
	}
	a.lastTrayClick = now
	a.trayClickTimer = time.AfterFunc(trayDoubleClick, func() {
		a.trayClickMu.Lock()
		a.trayClickTimer = nil
		a.trayClickMu.Unlock()
		a.showWindow()
	})
}

func (a *App) installDragOverlay() {
	if a.window == nil {
		return
	}
	a.window.ExecJS(dragOverlayJS())
	a.window.ExecJS(fileDropJS())
}

func (a *App) scheduleDragOverlay() {
	go func() {
		for range 40 {
			time.Sleep(250 * time.Millisecond)
			a.installDragOverlay()
			a.installExternalLinks()
			a.installLocalHomeGuard()
		}
	}()
}

func (a *App) requestQuit() {
	a.mu.Lock()
	a.quitting = true
	a.mu.Unlock()
	if a.app != nil {
		a.app.Quit()
	}
}

func main() {
	store := &settingsStore{cur: loadSettings()}
	api := &App{
		store: store,
		sleep: &sleepGuard{},
	}

	app := application.New(application.Options{
		Name:        "猛犸智能体",
		Description: "猛犸智能体",
		Services: []application.Service{
			application.NewService(api),
		},
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(assets),
		},
		Windows: application.WindowsOptions{
			DisableQuitOnLastWindowClosed: true,
		},
		Linux: application.LinuxOptions{
			DisableQuitOnLastWindowClosed: true,
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	})
	api.app = app
	attachOpenURLEventListener(app, api.OpenExternal)
	attachFileDropEventListener(app, api)
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(_ *application.ApplicationEvent) {
		applyAppIcon(app)
	})

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:                "猛犸智能体",
		Width:                1200,
		Height:               800,
		URL:                  "/",
		Frameless:            true,
		AllowSimpleEventEmit: true,
		EnableFileDrop:       true,
		BackgroundColour:     application.NewRGB(247, 248, 250),
	})
	api.window = win
	if os.Getenv("OCTOP_DESKTOP_DEVTOOLS") == "1" {
		win.OpenDevTools()
	}
	app.Event.On("desktop:toggle-maximise", func(_ *application.CustomEvent) {
		win.ToggleMaximise()
	})
	app.Event.On("desktop:minimise", func(_ *application.CustomEvent) {
		win.Minimise()
	})
	app.Event.On("desktop:close", func(_ *application.CustomEvent) {
		api.hideToTray()
	})
	// MAMMOTH-PATCH: AvatarDropdown「版本更新」→ portable hot-restart.
	app.Event.On("desktop:mammoth-update", func(_ *application.CustomEvent) {
		api.beginMammothUpdate()
	})
	installDragOverlay := func(_ *application.WindowEvent) { api.scheduleDragOverlay() }
	win.OnWindowEvent(events.Mac.WebViewDidFinishNavigation, installDragOverlay)
	win.OnWindowEvent(events.Windows.WebViewNavigationCompleted, installDragOverlay)
	win.OnWindowEvent(events.Linux.WindowLoadFinished, installDragOverlay)
	settingsWin := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "猛犸智能体 设置",
		Width:            settingsWindowWidth,
		Height:           settingsWindowOuterHeight(),
		URL:              "/?settings=1",
		Hidden:           true,
		Frameless:        true,
		AlwaysOnTop:      true,
		DisableResize:    true,
		BackgroundColour: application.NewRGB(255, 255, 255),
		Windows: application.WindowsWindow{
			HiddenOnTaskbar: true,
		},
	})
	api.settingsWindow = settingsWin
	app.Event.RegisterApplicationEventHook(events.Mac.ApplicationShouldHandleReopen, func(event *application.ApplicationEvent) {
		event.Cancel()
		restoreMainAfterDockClick(api.window, api.settingsWindow)
	})

	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		api.mu.Lock()
		quit := api.quitting
		api.mu.Unlock()
		if quit {
			return
		}
		e.Cancel()
		// Windows users read the X button as "quit"; hiding to the tray here is
		// what left several invisible instances running with no way out.
		if runtime.GOOS == "windows" {
			api.requestQuit()
			return
		}
		api.hideToTray()
	})
	win.OnWindowEvent(events.Common.WindowMinimise, func(_ *application.WindowEvent) {
		if api.store.get().MinimizeToTray {
			api.hideToTray()
		}
	})
	settingsWin.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		e.Cancel()
		settingsWin.Hide()
	})
	settingsWin.OnWindowEvent(events.Common.WindowLostFocus, func(_ *application.WindowEvent) {
		settingsWin.Hide()
	})

	tray := app.SystemTray.New()
	applyTrayIcon(tray)
	tray.SetTooltip("猛犸智能体")
	tray.AttachWindow(settingsWin).WindowOffset(6)
	showSettings := func() { tray.ShowWindow() }
	if trayLeftClickShowsSettings(runtime.GOOS) {
		tray.OnClick(showSettings)
	} else {
		tray.OnClick(func() { api.onTrayLeftClick() })
	}
	if trayNeedsQuitMenu(runtime.GOOS) {
		// Windows has no system-wide quit path (no ⌘Q equivalent) and the X
		// button used to only hide the window, so the tray menu is the only
		// place a normal user can find "quit".
		menu := application.NewMenu()
		menu.Add("打开猛犸智能体").OnClick(func(*application.Context) { api.showWindow() })
		menu.Add("设置").OnClick(func(*application.Context) { showSettings() })
		menu.AddSeparator()
		menu.Add("退出").OnClick(func(*application.Context) { api.requestQuit() })
		tray.SetMenu(menu)
		tray.OnRightClick(func() { tray.OpenMenu() })
	} else {
		tray.OnRightClick(showSettings)
	}

	if _, err := api.setAutostart(store.get().Autostart); err != nil {
		log.Printf("sync autostart: %v", err)
	}
	if err := api.sleep.set(store.get().PreventSleep); err != nil {
		log.Printf("enable sleep prevention: %v", err)
	}

	api.scheduleDragOverlay()
	go api.boot()

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
