      import { Call, Events } from "/wails/runtime.js";

      // Single copy source: assets/copy.json, embedded in Go (desktop_copy.go)
      // and served to this page as one table. `call` is declared below —
      // function declarations hoist, so this top-level await is safe.
      const copy = await call("GetCopy").catch(() => ({}));

      function applyWindowCopy(t) {
        for (const group of document.querySelectorAll(".window-controls")) {
          group.setAttribute("aria-label", t.winGroup);
        }
        for (const btn of document.querySelectorAll("[data-action]")) {
          const action = btn.getAttribute("data-action");
          if (action === "minimise") btn.setAttribute("aria-label", t.winMin);
          if (action === "toggle-maximise")
            btn.setAttribute("aria-label", t.winMax);
          if (action === "close") btn.setAttribute("aria-label", t.winClose);
        }
      }

      function applyCopy(locale) {
        const t = copy[locale] || copy.en;
        document.getElementById("title").textContent = t.title;
        document.getElementById("subtitle").textContent = t.subtitle;
        document.getElementById("l-lang").textContent = t.lang;
        document.getElementById("h-lang").textContent = t.langH;
        document.getElementById("l-auto").textContent = t.auto;
        document.getElementById("h-auto").textContent = t.autoH;
        document.getElementById("l-tray").textContent = t.tray;
        document.getElementById("h-tray").textContent = t.trayH;
        document.getElementById("l-sleep").textContent = t.sleep;
        document.getElementById("h-sleep").textContent = t.sleepH;
        document.getElementById("show-main").textContent = t.showMain;
        document.getElementById("quit").textContent = t.quit;
        document.getElementById("loading-hint").textContent = t.loadingHint;
        applyWindowCopy(t);
      }

      function emitWindowAction(action) {
        const invoke = window._wails?.invoke;
        if (typeof invoke === "function") {
          invoke(`wails:event:emit:desktop:${action}`);
          return;
        }
        Events.Emit("desktop:" + action);
      }

      document.body.addEventListener("click", (event) => {
        const btn = event.target.closest("[data-action]");
        if (!btn) return;
        const action = btn.getAttribute("data-action");
        if (
          document.documentElement.dataset.mode === "settings" &&
          action === "close"
        ) {
          call("HideSettings");
          return;
        }
        emitWindowAction(action);
      });

      async function call(name, ...args) {
        return Call.ByName("main.App." + name, ...args);
      }

      document.getElementById("mascot").src = "/mengma-logo.png";

      function current() {
        return {
          locale: document.getElementById("locale").value,
          autostart: document.getElementById("autostart").checked,
          minimizeToTray: document.getElementById("tray").checked,
          preventSleep: document.getElementById("sleep").checked,
        };
      }

      function settingsStatusEl() {
        return document.getElementById("settings-status");
      }

      async function persist() {
        let savedBefore;
        const errEl = settingsStatusEl();
        try {
          savedBefore = await call("GetSettingsStatus");
          const saved = await call("SaveSettings", {
            ...savedBefore,
            ...current(),
          });
          document.getElementById("autostart").checked = !!saved.autostart;
          document.getElementById("sleep").checked = !!saved.preventSleep;
          errEl.classList.add("hidden");
          applyCopy(saved.locale);
        } catch (error) {
          if (savedBefore) {
            document.getElementById("autostart").checked =
              !!savedBefore.autostart;
            document.getElementById("sleep").checked =
              !!savedBefore.preventSleep;
          }
          errEl.textContent = String(error);
          errEl.classList.remove("hidden");
        }
      }

      const settingsMode =
        new URLSearchParams(window.location.search).get("settings") === "1";
      const s = await call("GetSettingsStatus");
      const locale = s.locale || "en";
      document.getElementById("status").textContent =
        (copy[locale] || copy.en).statusWait;
      applyCopy(locale);

      if (settingsMode) {
        document.getElementById("loading-panel").classList.add("hidden");
        document.getElementById("settings-panel").classList.remove("hidden");
        document.getElementById("locale").value = locale;
        document.getElementById("autostart").checked = !!s.autostart;
        document.getElementById("tray").checked = s.minimizeToTray !== false;
        document.getElementById("sleep").checked = !!s.preventSleep;

        for (const id of ["locale", "autostart", "tray", "sleep"]) {
          document.getElementById(id).addEventListener("change", persist);
        }
        document
          .getElementById("show-main")
          .addEventListener("click", () => call("ShowMain"));
        document
          .getElementById("quit")
          .addEventListener("click", () => call("Quit"));
      }

      Events.On("desktop:status", (ev) => {
        const msg =
          typeof ev === "string"
            ? ev
            : ev?.data ?? ev?.payload ?? JSON.stringify(ev);
        const text = String(msg);
        document.getElementById("status").textContent = text;
        const stuck = /未就绪|did not become ready|无法连接|Could not connect/.test(
          text,
        );
        document.getElementById("loading-panel").classList.toggle("is-error", stuck);
      });
