"use strict";

const get = (id) => document.getElementById(id);
const node = (tag, text, className) => {
  const el = document.createElement(tag);
  if (text) el.textContent = text;
  if (className) el.className = className;
  return el;
};

const sessionKey = "infra-composer-session";
const fragment = location.hash.slice(1);
if (/^[A-Za-z0-9_-]{43}$/.test(fragment)) {
  sessionStorage.setItem(sessionKey, fragment);
  history.replaceState(null, "", location.pathname);
}
const token = sessionStorage.getItem(sessionKey);
let revision = 0;
let panelKey = "";
let activityKey = "";
let authText = "";
let planShown = false;
let current;
let closed = false;

const actions = {
  setup: "Setup", build: "Build", preview: "Preview", deploy: "Deploy", status: "Status",
  report: "Artifact report", "github-setup": "GitHub setup", destroy: "Destroy stack",
  "clean-cache": "Clear build cache",
};
const statuses = {
  running: "In progress", stopping: "Stopping", cancelled: "Stopped",
  complete: "Complete", failed: "Failed",
};
const completed = (status) => ["complete", "failed", "cancelled"].includes(status);
const clean = (text) => text.replace(/\x1b\[[0-?]*[ -/]*[@-~]/g, "");
const authHeaders = {Authorization: "Bearer " + token};

function button(label, variant, type = "button") {
  const control = node("button", label, "button " + variant);
  control.type = type;
  return control;
}

function showConnection(text) {
  get("connection").textContent = text;
  get("connection").hidden = !text;
}

async function post(path, payload = null) {
  return fetch(path, {
    method: "POST",
    headers: {...authHeaders, "Content-Type": "application/json"},
    body: payload === null ? null : JSON.stringify(payload),
  });
}

async function answer(prompt, value) {
  const controls = Array.from(get("interaction").querySelectorAll("button"));
  controls.forEach((control) => { control.disabled = true; });
  try {
    const response = await post("/api/answer", {id: prompt.id, value});
    if (!response.ok && response.status !== 409) throw new Error("The wizard couldn’t accept this response.");
    await refresh();
  } catch (error) {
    showConnection(error.message);
    controls.forEach((control) => { control.disabled = false; });
  }
}

function renderForm(container, prompt) {
  const form = node("form");
  let field;
  if (prompt.kind === "select") {
    field = node("select");
    field.required = true;
    if (!prompt.default) {
      const placeholder = node("option", "Choose an option");
      placeholder.value = "";
      placeholder.disabled = true;
      placeholder.selected = true;
      field.append(placeholder);
    }
    for (const option of prompt.options) {
      const item = node("option", option.label);
      item.value = option.value;
      item.selected = option.value === prompt.default;
      field.append(item);
    }
  } else {
    field = node("input");
    field.type = prompt.kind === "secret" ? "password" : "text";
    field.autocomplete = "off";
    field.spellcheck = false;
    field.required = prompt.kind === "secret" || prompt.kind === "deliberate";
    field.maxLength = 4096;
    if (prompt.default) {
      field.placeholder = prompt.default;
      form.append(node("div", "Default · " + prompt.default, "muted"));
    }
  }
  field.setAttribute("aria-labelledby", "panel-title");
  form.append(field, button(prompt.kind === "deliberate" ? "Confirm" : "Continue", "primary", "submit"));
  form.addEventListener("submit", (event) => {
    event.preventDefault();
    const value = field.value;
    if (prompt.kind === "secret") field.value = "";
    answer(prompt, value);
  });
  container.append(form);
  field.focus();
}

function renderReview(container, prompt) {
  const table = node("table", "", "review");
  const caption = node("caption", "Deployment settings", "sr-only");
  const body = node("tbody");
  prompt.fields.forEach((field, index) => {
    const row = node("tr");
    const label = node("th", field.label);
    label.scope = "row";
    const edit = node("td");
    if (field.editable) {
      const control = button("Edit", "outline small");
      control.setAttribute("aria-label", "Edit " + field.label);
      control.addEventListener("click", () => answer(prompt, String(index)));
      edit.append(control);
    }
    row.append(label, node("td", field.value || "—"), edit);
    body.append(row);
  });
  table.append(caption, body);
  const proceed = button("Continue to sign-in", "primary");
  proceed.addEventListener("click", () => answer(prompt, "continue"));
  container.append(table, proceed);
}

function renderPrompt(view) {
  const prompt = view.prompt;
  const key = prompt ? "prompt:" + prompt.id : view.status + ":" + view.phaseTitle;
  if (panelKey === key) return;
  panelKey = key;
  const content = get("interaction");
  content.replaceChildren();
  const title = get("panel-title");
  if (!prompt) {
    title.textContent = view.status === "complete" ? (actions[view.action] || "Wizard") + " complete" :
      view.status === "failed" ? "The wizard stopped" :
      view.status === "cancelled" ? "Wizard stopped" :
      view.status === "stopping" ? "Stopping the current operation" : view.phaseTitle;
    if (view.error) content.append(node("p", view.error, "problem"));
    if (view.status === "stopping") content.append(node("p", "Waiting for the operation to exit and release its locks."));
    return;
  }
  title.textContent = prompt.label;
  if (prompt.message) content.append(node("p", prompt.message, prompt.kind === "review" ? "problem" : ""));
  if (prompt.kind === "review") {
    renderReview(content, prompt);
  } else if (prompt.kind === "confirm") {
    const buttons = node("div", "", "buttons");
    for (const option of prompt.options) {
      const control = button(option.label, option.value === "yes" ? "primary" : "outline");
      control.addEventListener("click", () => answer(prompt, option.value));
      buttons.append(control);
    }
    content.append(buttons);
  } else {
    renderForm(content, prompt);
  }
}

function renderSignIn(text) {
  if (authText === text) return;
  authText = text;
  get("sign-in").hidden = !text;
  const links = get("sign-in-actions");
  links.replaceChildren();
  get("sign-in-output").textContent = clean(text);
  const urls = new Set(clean(text).match(/https:\/\/[^\s<>"']+/g) || []);
  for (const candidate of urls) {
    try {
      const url = new URL(candidate.replace(/[).,]+$/, ""));
      const host = url.hostname;
      if (host.endsWith(".awsapps.com") || host.endsWith(".amazonaws.com") ||
          (host === "github.com" && url.pathname === "/login/device")) {
        const link = node("a", host === "github.com" ? "Open GitHub sign-in" : "Open AWS sign-in", "button primary");
        link.href = url.href;
        link.target = "_blank";
        link.rel = "noopener noreferrer";
        links.append(link);
      }
    } catch {
      // Provider output is still available as text.
    }
  }
}

function renderPlan(view) {
  if (!view.reviewed || planShown) return;
  planShown = true;
  get("preview").hidden = false;
  const changes = view.changes || [];
  const count = view.total;
  get("preview-count").textContent = count === 0 ? "No resource operations planned." :
    count + (count === 1 ? " resource operation" : " resource operations") +
    (changes.length < count ? " · Showing the first " + changes.length : "");
  if (changes.length === 0) return;
  const table = node("table", "", "changes");
  const head = node("thead");
  const row = node("tr");
  row.append(node("th", "Operation"), node("th", "Resource"));
  head.append(row);
  const body = node("tbody");
  for (const change of changes) {
    const item = node("tr");
    item.append(node("td", change.operation), node("td", change.resource));
    body.append(item);
  }
  table.append(head, body);
  get("preview-changes").append(table);
}

function render(view) {
  current = view;
  get("project").textContent = view.title;
  get("action").textContent = actions[view.action] || "Deployment wizard";
  const stop = get("stop");
  stop.hidden = false;
  stop.textContent = completed(view.status) ? "Close wizard" : "Stop wizard";
  stop.disabled = view.status === "stopping";
  const status = get("status");
  status.dataset.status = view.status;
  status.textContent = view.prompt ? "Waiting for you" : statuses[view.status];

  const phases = Array.from(get("phases").children);
  const active = phases.findIndex((step) => step.dataset.phase === view.phase);
  phases.forEach((step, index) => {
    const selected = index === active;
    const visited = (view.visited || []).includes(step.dataset.phase);
    step.hidden = !visited && (
      (step.dataset.phase === "preview" && view.action && !["deploy", "preview"].includes(view.action)) ||
      (step.dataset.phase === "apply" && view.action && view.action !== "deploy")
    );
    step.classList.toggle("active", selected && view.status !== "complete");
    step.classList.toggle("passed", visited && (!selected || view.status === "complete"));
    if (selected && view.status === "running") step.setAttribute("aria-current", "step");
    else step.removeAttribute("aria-current");
  });
  renderPrompt(view);
  renderSignIn(view.signIn || "");
  renderPlan(view);

  const activity = view.activity || [];
  const serial = JSON.stringify(activity);
  if (serial !== activityKey) {
    activityKey = serial;
    const log = get("events");
    log.replaceChildren(...(activity.length ? activity.map((line) => node("li", clean(line))) :
      [node("li", "Waiting for the wizard to start.", "muted")]));
    log.scrollTop = log.scrollHeight;
  }
}

async function refresh() {
  if (closed) return;
  const response = await fetch("/api/state?after=" + revision, {headers: authHeaders});
  if (response.status === 401 || response.status === 403) {
    closed = true;
    get("status").textContent = "Session unavailable";
    get("panel-title").textContent = "Open the URL from your terminal";
    return;
  }
  if (response.status === 204) return;
  if (!response.ok) throw new Error("The local wizard is unavailable.");
  const view = await response.json();
  if (view.revision > revision) {
    revision = view.revision;
    showConnection("");
    render(view);
  }
}

async function poll() {
  try {
    await refresh();
  } catch {
    showConnection("Connection interrupted. Check the terminal. Retrying…");
  }
  if (!closed) setTimeout(poll, 400);
}

get("stop").addEventListener("click", async () => {
  if (current.phase === "apply" && current.status === "running" &&
      !confirm("Stop this operation? Changes already sent to the cloud may still finish.")) return;
  try {
    const response = await post("/api/stop");
    if (!response.ok) throw new Error("The wizard couldn’t stop. Check the terminal.");
    if (completed(current.status)) {
      closed = true;
      sessionStorage.removeItem(sessionKey);
      get("stop").hidden = true;
      showConnection("");
      get("interaction").replaceChildren(node("p", "Local session closed. You can close this tab."));
    }
  } catch (error) {
    showConnection(error.message);
  }
});
poll();
