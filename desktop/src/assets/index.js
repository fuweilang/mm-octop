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
        document.getElementById("alt-port").textContent = t.altPort;
        shellCopy = t;
        document.getElementById("legacy-check").textContent = t.legacyCheck;
        document.getElementById("legacy-remove").textContent = t.legacyTrash;
        document.getElementById("legacy-later").textContent = t.legacyLater;
        applyWindowCopy(t);
      }

      /** Copy table of the active locale; set by applyCopy. */
      let shellCopy = copy.en;

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

        // Legacy 「猛犸AI」: one row with two states. Removal is always explicit —
        // the shell never deletes another app on its own.
        const legacyRow = document.getElementById("legacy-row");
        const legacyActions = document.getElementById("legacy-actions");
        const legacyCheckBtn = document.getElementById("legacy-check");
        const legacyStatus = document.getElementById("settings-status");
        const renderLegacy = (path) => {
          const found = !!path;
          document.getElementById("l-legacy").textContent = found
            ? shellCopy.legacyTitle
            : shellCopy.legacyCheckLabel;
          document.getElementById("h-legacy").textContent = found
            ? shellCopy.legacyHint
            : shellCopy.legacyCheckHint;
          legacyActions.classList.toggle("hidden", !found);
          legacyCheckBtn.classList.toggle("hidden", found);
          legacyRow.classList.toggle("legacy-found", found);
        };
        const showLegacyStatus = (text) => {
          if (!text) return;
          legacyStatus.textContent = text;
          legacyStatus.classList.remove("hidden");
        };
        let legacyPath = "";
        try {
          const report = await call("CheckLegacyApp");
          legacyPath = report?.path || "";
        } catch {
          legacyPath = "";
        }
        renderLegacy(legacyPath);
        Events.On("desktop:legacy-app", (ev) => {
          const path =
            typeof ev === "string"
              ? ev
              : ev?.data ?? ev?.payload ?? String(ev ?? "");
          if (path) {
            legacyPath = String(path);
            renderLegacy(legacyPath);
          }
        });
        legacyCheckBtn.addEventListener("click", async () => {
          const report = await call("CheckLegacyApp").catch(() => null);
          legacyPath = report?.path || "";
          renderLegacy(legacyPath);
        });
        document
          .getElementById("legacy-remove")
          .addEventListener("click", async () => {
            const reason = await call("RemoveLegacyApp").catch(() => "");
            if (!reason) {
              renderLegacy("");
              showLegacyStatus(shellCopy.legacyRemoved);
              return;
            }
            // The shell could not do it: reveal the app so the user can finish
            // by hand, and say why.
            await call("OpenLegacyAppFolder").catch(() => {});
            showLegacyStatus(shellCopy[reason] || reason);
          });
        document
          .getElementById("legacy-later")
          .addEventListener("click", async () => {
            await call("DismissLegacyApp").catch(() => {});
            renderLegacy("");
            showLegacyStatus(shellCopy.legacyDeferred);
          });
      }

      // Port conflicts are the one case where the shell must not decide alone:
      // another port is a different origin and would sign the user out.
      const showAltPort = () => {
        if (settingsMode) return;
        document.getElementById("loading-panel").classList.remove("hidden");
        document.getElementById("alt-port").classList.remove("hidden");
      };
      Events.On("desktop:port-blocked", showAltPort);
      // The event is fire-and-forget; re-read the state so a reload still
      // shows the reason and the button after boot already reported it.
      if (!settingsMode && (await call("IsPortBlocked").catch(() => false))) {
        const reason = await call("PortBlockedMessage").catch(() => "");
        if (reason) document.getElementById("status").textContent = reason;
        showAltPort();
      }
      document
        .getElementById("alt-port")
        .addEventListener("click", async (event) => {
          const btn = event.currentTarget;
          btn.classList.add("hidden");
          await call("RetryWithAlternatePort").catch(() => {});
        });

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
