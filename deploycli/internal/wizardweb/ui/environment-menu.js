"use strict";

(() => {
  const get = id => document.getElementById(id);
  const node = (tag, text) => {
    const element = document.createElement(tag);
    if (text) element.textContent = text;
    return element;
  };
  const menu = get("environment-menu");
  const toggle = get("main-menu-toggle");
  const items = get("main-menu-items");
  const active = get("active-environment");
  const dialog = get("environment-dialog");
  const fields = get("environment-dialog-fields");
  const error = get("environment-dialog-error");
  const submit = get("environment-submit");
  const selectable = state => state?.status === "running" && ["actions", "environment"].includes(state.prompt?.kind);
  let view;
  let request;

  function showMenu(show, focus = false) {
    items.hidden = !show;
    toggle.setAttribute("aria-expanded", String(show));
    if (focus) (show ? items.querySelector("button") : toggle).focus();
  }

  function showDialog(mode) {
    if (!selectable(view)) return;
    pauseCountdown();
    request = {mode, id: view.prompt.id};
    showMenu(false);
    error.hidden = true;
    fields.replaceChildren();
    const creating = mode === "new";
    get("environment-dialog-title").textContent = creating ? "New Environment" : "Open Environment";
    submit.textContent = creating ? "Create" : "Open";
    submit.disabled = false;
    if (creating) {
      const label = node("label", "Environment name");
      const input = node("input");
      input.name = "environment";
      input.type = "text";
      input.required = true;
      input.maxLength = 31;
      input.placeholder = "e.g. dev";
      input.autocomplete = "off";
      input.spellcheck = false;
      label.append(input);
      fields.append(label, node("p", "Use lowercase letters, digits, or hyphens. Start with a letter."));
    } else {
      const environments = view.environments || [];
      const deployed = environments.filter(environment => environment.status === "deployed");
      const saved = environments.filter(environment => environment.status !== "deployed");
      const initial = environments.some(environment => environment.name === view.environment)
        ? view.environment : (deployed[0] || saved[0])?.name;
      for (const [title, options] of [["Recorded deployments", deployed], ["Saved setups", saved]]) {
        if (!options.length) continue;
        const list = node("fieldset");
        list.append(node("legend", title));
        for (const environment of options) {
          const row = node("label");
          row.className = "environment-option";
          const input = node("input");
          input.type = "radio";
          input.name = "environment";
          input.value = environment.name;
          input.required = true;
          input.checked = environment.name === initial;
          row.append(input, node("span", environment.name));
          if (environment.status === "draft") row.append(node("small", "Draft"));
          list.append(row);
        }
        fields.append(list);
      }
      if (!environments.length) {
        fields.append(node("p", "No environments saved for this project yet. Use New Environment to get started."));
        submit.disabled = true;
      }
    }
    dialog.showModal();
    (fields.querySelector("input[type=text], input:checked") || get("environment-cancel")).focus();
  }

  toggle.addEventListener("click", () => {
    if (selectable(view)) showMenu(items.hidden, true);
  });
  items.addEventListener("click", event => {
    const option = event.target.closest("[data-environment-mode]");
    if (option) showDialog(option.dataset.environmentMode);
  });
  document.addEventListener("pointerdown", event => {
    if (!menu.contains(event.target)) showMenu(false);
  });
  menu.addEventListener("keydown", event => {
    if (event.key === "Escape" && !items.hidden) {
      event.preventDefault();
      showMenu(false, true);
    }
  });
  get("environment-cancel").addEventListener("click", () => dialog.close());
  dialog.addEventListener("close", () => {
    request = undefined;
    if (!toggle.disabled) toggle.focus();
  });
  get("environment-dialog-form").addEventListener("submit", async event => {
    event.preventDefault();
    if (!request || submit.disabled) return;
    const selection = request;
    const name = String(new FormData(event.currentTarget).get("environment") || "").trim();
    error.hidden = true;
    submit.disabled = true;
    try {
      const response = await post("/api/environment", {...selection, name});
      if (!response.ok) throw new Error((await response.text()).trim());
      if (request === selection && dialog.open) dialog.close();
      await refresh();
    } catch (err) {
      if (request !== selection) return;
      error.textContent = err.message || "Couldn’t select this environment.";
      error.hidden = false;
    } finally {
      if (request === selection) submit.disabled = false;
    }
  });

  window.renderEnvironmentMenu = state => {
    view = state;
    menu.hidden = !state;
    toggle.disabled = !selectable(state);
    toggle.title = toggle.disabled ? "Return to Actions to change the environment" : "Main menu";
    active.hidden = !state;
    active.textContent = state?.environment || "No environment";
    active.title = state?.environment ? "Environment: " + state.environment : "";
    if (toggle.disabled) {
      showMenu(false);
      if (dialog.open) dialog.close();
    } else if (dialog.open && request?.id !== state.prompt.id) {
      dialog.close();
    }
  };
})();
