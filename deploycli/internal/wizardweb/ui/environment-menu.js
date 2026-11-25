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
  const tabs = get("environment-tabs");
  const projectMenu = get("project-menu-section");
  const removeAll = projectMenu.querySelector("button");
  const dialog = get("environment-dialog");
  const fields = get("environment-dialog-fields");
  const error = get("environment-dialog-error");
  const submit = get("environment-submit");
  const atActionMenu = state => state?.status === "running" && state.prompt?.kind === "actions";
  const selectable = state => atActionMenu(state) ||
    (state?.status === "running" && state.prompt?.kind === "environment") ||
    (state && ["complete", "failed", "cancelled"].includes(state.status));
  const selectionID = state => state.prompt?.id ?? state.revision;
  let view;
  let request;
  let tabsKey;

  function showMenu(show, focus = false) {
    items.hidden = !show;
    toggle.setAttribute("aria-expanded", String(show));
    if (focus) (show ? items.querySelector("button") : toggle).focus();
  }

  function showDialog(mode) {
    if (!selectable(view) || (mode === "destroy-all" && removeAll.disabled)) return;
    pauseCountdown();
    request = {mode, id: selectionID(view)};
    showMenu(false);
    error.hidden = true;
    fields.replaceChildren();
    const projectTeardown = mode === "destroy-all";
    get("environment-dialog-title").textContent = {
      new: "New Environment", open: "Open Environment", "destroy-all": "Remove All Environments and Index",
    }[mode];
    submit.textContent = {new: "Create", open: "Open", "destroy-all": "Review teardown"}[mode];
    submit.classList.toggle("project-teardown-submit", projectTeardown);
    submit.disabled = false;
    if (projectTeardown) {
      fields.append(node("p", "Review removal of every saved environment in this project, followed by the shared index. The wizard will ask you to confirm the targets before removing anything."));
    } else if (mode === "new") {
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
      const response = selection.mode === "destroy-all"
        ? await post("/api/project-teardown", {id: selection.id})
        : await post("/api/environment", {...selection, name});
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

  function renderTabs(state, available) {
    const names = state?.openEnvironments || [];
    const key = JSON.stringify([names, state?.environment, available]);
    if (key === tabsKey) return;
    tabsKey = key;
    const focused = tabs.contains(document.activeElement) ? document.activeElement.textContent : "";
    tabs.hidden = names.length === 0;
    tabs.replaceChildren(...names.map(name => {
      const tab = node("button", name);
      tab.type = "button";
      tab.className = "environment-tab";
      tab.title = name;
      tab.disabled = !available;
      if (name === state.environment) tab.setAttribute("aria-current", "true");
      return tab;
    }));
    if (focused && available) {
      [...tabs.children].find(tab => tab.textContent === focused)?.focus();
    }
  }

  tabs.addEventListener("click", async event => {
    const tab = event.target.closest(".environment-tab");
    if (!tab || !selectable(view) || tab.textContent === view.environment) return;
    pauseCountdown();
    try {
      const response = await post("/api/environment", {id: selectionID(view), mode: "switch", name: tab.textContent});
      if (!response.ok && response.status !== 409) throw new Error((await response.text()).trim());
      await refresh();
    } catch (err) {
      showConnection(err.message || "Couldn’t switch environments.");
    }
  });

  window.renderEnvironmentMenu = state => {
    view = state;
    menu.hidden = !state;
    const available = Boolean(selectable(state));
    toggle.disabled = !available;
    toggle.title = available ? "Main menu" : "Finish or stop the operation to change the environment";
    projectMenu.hidden = !state?.canRemoveAll;
    removeAll.disabled = !atActionMenu(state);
    removeAll.title = removeAll.disabled ? "Return to Actions to review project teardown" : "";
    renderTabs(state, available);
    if (toggle.disabled) {
      showMenu(false);
      if (dialog.open) dialog.close();
    } else if (dialog.open && request?.id !== selectionID(state)) {
      dialog.close();
    }
  };
})();
