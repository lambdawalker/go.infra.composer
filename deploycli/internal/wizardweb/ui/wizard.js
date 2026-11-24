"use strict";

const get = (id) => document.getElementById(id);
const node = (tag, text, className) => {
  const el = document.createElement(tag);
  if (text) el.textContent = text;
  if (className) el.className = className;
  return el;
};

const themeControl = get("theme");
const themeKey = "infra-composer-theme";
const savedTheme = sessionStorage.getItem(themeKey);
themeControl.value = savedTheme === "light" || savedTheme === "dark" ? savedTheme : "system";

function applyTheme() {
  const theme = themeControl.value;
  if (theme === "system") {
    delete document.documentElement.dataset.theme;
    sessionStorage.removeItem(themeKey);
  } else {
    document.documentElement.dataset.theme = theme;
    sessionStorage.setItem(themeKey, theme);
  }
}

applyTheme();
themeControl.addEventListener("change", applyTheme);

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
let planRun;
let current;
let closed = false;
let countdownTick;
let answeringPrompt;
let vaultLocked = false;
let focusBeforeLock;
let activityTimer;
let lastActivityReport = -Infinity;
const footer = get("action-bar");
const promptActions = get("prompt-actions");
const stopControl = get("stop");

const actions = {
  setup: "Setup", build: "Build", preview: "Preview", deploy: "Deploy", status: "Status",
  report: "Artifact report", "github-setup": "GitHub setup", destroy: "Remove environment",
  "destroy-all": "Remove all environments", "github-teardown": "Remove GitHub configuration",
  "clean-cache": "Clear build cache",
};
const statuses = {
  running: "In progress", stopping: "Stopping", cancelled: "Stopped",
  complete: "Complete", failed: "Failed",
};
const completed = (status) => ["complete", "failed", "cancelled"].includes(status);
const canClose = (view) => completed(view.status) || view.prompt?.kind === "actions";
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

async function post(path, payload = null, signal) {
  return fetch(path, {
    method: "POST",
    headers: {...authHeaders, "Content-Type": "application/json"},
    body: payload === null ? null : JSON.stringify(payload),
    signal,
  });
}

function reportActivity() {
  if (closed || vaultLocked || document.hidden) return;
  lastActivityReport = performance.now();
  post("/api/activity").catch(() => {});
}

function noteActivity(event) {
  if (!event.isTrusted || closed || vaultLocked || document.hidden) return;
  if (activityTimer !== undefined) return;
  const delay = 1000 - (performance.now() - lastActivityReport);
  if (delay <= 0) {
    reportActivity();
    return;
  }
  activityTimer = setTimeout(() => {
    activityTimer = undefined;
    reportActivity();
  }, delay);
}

function renderVaultLock(locked) {
  if (vaultLocked === locked) return;
  vaultLocked = locked;
  const screen = get("vault-lock");
  const wizard = get("wizard");
  const input = get("vault-unlock-input");
  if (locked) focusBeforeLock = document.activeElement;
  screen.hidden = !locked;
  wizard.hidden = locked;
  footer.hidden = locked;
  input.value = "";
  get("vault-unlock-error").hidden = true;
  if (locked) {
    resetCountdown();
    wizard.querySelectorAll('input[type="password"]').forEach(field => { field.value = ""; });
    input.focus();
  } else {
    focusBeforeLock?.focus();
    focusBeforeLock = undefined;
  }
}

get("vault-unlock-form").addEventListener("submit", async event => {
  event.preventDefault();
  const field = get("vault-unlock-input");
  const submit = get("vault-unlock-button");
  const error = get("vault-unlock-error");
  const passphrase = field.value;
  field.value = "";
  submit.disabled = true;
  error.hidden = true;
  try {
    const response = await post("/api/unlock", {passphrase});
    if (response.status === 422) {
      await refresh();
      if (vaultLocked) throw new Error("The vault passphrase didn’t match. Try again.");
      return;
    }
    if (!response.ok) throw new Error("Couldn’t unlock the wizard. Try again.");
    await refresh();
  } catch (err) {
    error.textContent = err.message;
    error.hidden = false;
    field.focus();
  } finally {
    submit.disabled = false;
  }
});

function resetCountdown() {
  clearInterval(countdownTick);
  countdownTick = undefined;
  get("countdown").hidden = true;
  get("pause-countdown").hidden = false;
}

function pauseCountdown() {
  if (countdownTick === undefined) return;
  clearInterval(countdownTick);
  countdownTick = undefined;
  get("countdown-text").textContent = "Countdown paused";
  const pause = get("pause-countdown");
  if (document.activeElement === pause) get("wizard-continue")?.focus();
  pause.hidden = true;
}

function startCountdown(prompt) {
  const form = get("wizard-form");
  if (!form || !prompt.countdownSeconds || document.hidden) return;
  let remaining = prompt.countdownSeconds;
  const label = prompt.kind === "actions" ? "Select Preview in" : "Using defaults in";
  const update = () => { get("countdown-text").textContent = label + " " + remaining + "s"; };
  update();
  get("countdown").hidden = false;
  countdownTick = setInterval(() => {
    if (document.hidden || !form.isConnected || current.prompt?.id !== prompt.id) {
      pauseCountdown();
      return;
    }
    remaining--;
    if (remaining === 0) {
      resetCountdown();
      form.requestSubmit(get("wizard-continue"));
      return;
    }
    update();
  }, 1000);
}

get("pause-countdown").addEventListener("click", pauseCountdown);
for (const event of ["keydown", "pointerdown", "input", "wheel"]) {
  document.addEventListener(event, event => {
    if (event.target !== get("pause-countdown")) pauseCountdown();
  }, {capture: true, passive: true});
}
for (const event of ["keydown", "pointerdown", "pointermove", "input", "wheel"]) {
  document.addEventListener(event, noteActivity, {capture: true, passive: true});
}
document.addEventListener("visibilitychange", () => {
  if (document.hidden) pauseCountdown();
  else refresh().catch(() => {});
});
new ResizeObserver(() => {
  document.documentElement.style.setProperty("--action-bar-size", footer.offsetHeight + "px");
}).observe(footer);

function addSubmit(form, label = "Continue") {
  form.id = "wizard-form";
  const submit = button(label, "primary", "submit");
  submit.id = "wizard-continue";
  submit.setAttribute("form", form.id);
  promptActions.append(submit);
}

async function answer(prompt, value, action) {
  if (answeringPrompt === prompt.id) return;
  answeringPrompt = prompt.id;
  resetCountdown();
  const controls = document.querySelectorAll("#interaction button, #prompt-actions button");
  controls.forEach((control) => { control.disabled = true; });
  try {
    const payload = prompt.kind === "form" ? {id: prompt.id, values: value, action} : {id: prompt.id, value};
    const response = await post("/api/answer", payload);
    if (!response.ok && response.status !== 409) throw new Error("The wizard couldn’t accept this response.");
    await refresh();
  } catch (error) {
    showConnection(error.message);
  } finally {
    controls.forEach((control) => { control.disabled = false; });
    if (answeringPrompt === prompt.id) answeringPrompt = undefined;
  }
}

function renderSingleInput(container, prompt) {
  const form = node("form");
  const field = node("input");
  field.type = prompt.kind === "secret" ? "password" : "text";
  field.autocomplete = "off";
  field.spellcheck = false;
  field.required = prompt.kind === "secret" || prompt.kind === "deliberate";
  field.maxLength = 4096;
  if (prompt.default) {
    field.placeholder = prompt.default;
    form.append(node("div", "Default · " + prompt.default, "muted"));
  }
  field.setAttribute("aria-labelledby", "panel-title");
  form.append(field);
  addSubmit(form, prompt.kind === "deliberate" ? "Confirm" : "Continue");
  form.addEventListener("submit", (event) => {
    event.preventDefault();
    const value = field.value;
    if (prompt.kind === "secret") field.value = "";
    answer(prompt, value);
  });
  container.append(form);
  field.focus();
}

function renderActions(container, prompt) {
  const form = node("form", "", "wide-form");
  const grid = node("fieldset", "", "action-grid");
  grid.append(node("legend", prompt.label, "sr-only"));
  prompt.options.forEach((option, index) => {
    const card = node("label", "", "action-card");
    const radio = node("input");
    const title = node("span", option.label, "action-title");
    const description = node("span", option.description, "action-description");
    title.id = "action-title-" + index;
    description.id = "action-description-" + index;
    radio.type = "radio";
    radio.name = "action";
    radio.value = option.value;
    radio.required = true;
    radio.checked = option.value === prompt.default;
    radio.setAttribute("aria-labelledby", title.id);
    radio.setAttribute("aria-describedby", description.id);
    card.append(radio, title, description);
    grid.append(card);
  });
  form.append(grid);
  addSubmit(form);
  form.addEventListener("submit", (event) => {
    event.preventDefault();
    answer(prompt, new FormData(form).get("action"));
  });
  container.append(form);
  grid.querySelector(":checked")?.focus();
}

function renderSettings(container, prompt) {
  const form = node("form", "", "wide-form settings-form");
  form.noValidate = true;
  const groups = new Map();
  const previews = new Map();
  const feedback = new Map();
  prompt.fields.forEach((field, index) => {
    if (!groups.has(field.group)) {
      const group = node("fieldset", "", "settings-group");
      group.dataset.group = field.group;
      group.append(node("legend", field.group));
      groups.set(field.group, group);
      form.append(group);
    }
    const row = node("div", "", "settings-field");
    row.dataset.field = field.name;
    const id = "setting-" + index;
    const label = node("label", field.label);
    label.htmlFor = id;
    let control;
    if (field.kind === "select" || field.options?.length) {
      control = node("select");
      control.append(new Option("Choose an option", "", false, true));
      for (const option of field.options || []) {
        control.append(new Option(option.label, option.value, false, option.value === field.value));
      }
    } else {
      control = node("input");
      const password = field.kind === "password";
      control.type = password ? "password" : "text";
      control.value = password ? "" : field.value;
      if (password && field.saved) control.placeholder = "*****";
      control.maxLength = 4096;
      control.autocomplete = password ? "new-password" : "off";
      control.spellcheck = false;
      if (field.kind === "email") control.inputMode = "email";
      if (field.kind === "number") control.inputMode = "numeric";
    }
    control.id = id;
    if (field.readOnly) {
      control.readOnly = true;
    } else {
      control.name = field.name;
    }
    row.append(label, control);
    const descriptions = [];
    if (field.help) {
      const help = node("div", field.help, "field-help");
      help.id = id + "-help";
      descriptions.push(help.id);
      row.append(help);
    }
    if (field.composed) {
      if (field.readOnly) {
        control.placeholder = "Calculated from project inputs";
        previews.set(field.name, (value) => { control.value = value; });
      } else {
        const preview = node("div", "", "field-help");
        preview.id = id + "-preview";
        preview.setAttribute("aria-live", "polite");
        descriptions.push(preview.id);
        row.append(preview);
        previews.set(field.name, (value) => {
          preview.textContent = value ? "Resolved value: " + value : "";
        });
      }
    }
    const error = node("div", "", "field-error");
    error.id = id + "-error";
    error.setAttribute("aria-live", "polite");
    descriptions.push(error.id);
    row.append(error);
    const showError = (message) => {
      error.textContent = message;
      error.hidden = !message;
      if (message) control.setAttribute("aria-invalid", "true");
      else control.removeAttribute("aria-invalid");
    };
    showError(field.error || "");
    feedback.set(field.name, showError);
    control.setAttribute("aria-describedby", descriptions.join(" "));
    if (field.action) {
      const action = button(field.action.label, "outline small");
      action.addEventListener("click", () => {
        answer(prompt, Object.fromEntries(new FormData(form)), field.action.value);
      });
      row.append(action);
    }
    groups.get(field.group).append(row);
  });
  const hasErrors = prompt.fields.some((field) => field.error);
  if (hasErrors) {
    const warning = node("p", "Check the highlighted fields and continue.", "problem");
    warning.setAttribute("role", "alert");
    form.prepend(warning);
  }
  addSubmit(form);
  form.addEventListener("submit", (event) => {
    event.preventDefault();
    answer(prompt, Object.fromEntries(new FormData(form)));
  });
  container.append(form);
  if (prompt.previewFields) {
    const touched = new Set();
    const submittedErrors = new Set(prompt.fields.filter(field => field.error).map(field => field.name));
    const previewFields = new Set(prompt.previewFields || []);
    let timer;
    let request;
    async function updatePreviews() {
      if (!form.isConnected) return;
      request = new AbortController();
      const {signal} = request;
      const data = new FormData(form);
      const values = Object.fromEntries(Array.from(previewFields, name => [name, data.get(name) ?? ""]));
      try {
        const response = await post("/api/form-preview", {id: prompt.id, values}, signal);
        if (!response.ok) return;
        const resolved = await response.json();
        if (signal.aborted || !form.isConnected) return;
        previews.forEach((render, name) => render(resolved.values?.[name] || ""));
        for (const name of touched) feedback.get(name)?.(resolved.errors?.[name] || "");
      } catch {
        // Continue still validates the complete form if preview is unavailable.
      }
    }
    const validateField = (event) => {
      const name = event.target.name;
      if (!previewFields.has(name)) return;
      if (event.type === "focusout" && submittedErrors.has(name) && !touched.has(name)) return;
      touched.add(name);
      request?.abort();
      clearTimeout(timer);
      timer = setTimeout(updatePreviews, 160);
    };
    form.addEventListener("input", validateField);
    form.addEventListener("focusout", validateField);
    updatePreviews();
  }
  if (hasErrors) form.querySelector('[aria-invalid="true"]')?.focus();
}

function addCopyDiagnostics(controls, content, diagnostics) {
  const copy = button("Copy diagnostics", "outline");
  const feedback = node("span", "", "muted");
  feedback.setAttribute("role", "status");
  const manual = node("textarea", "", "diagnostics-text");
  manual.readOnly = true;
  manual.hidden = true;
  manual.setAttribute("aria-label", "Diagnostics");
  manual.value = diagnostics;
  copy.addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(diagnostics);
      feedback.textContent = "Diagnostics copied.";
      manual.hidden = true;
    } catch {
      feedback.textContent = "Press Ctrl+C or Command+C to copy the selected diagnostics.";
      manual.hidden = false;
      manual.focus();
      manual.select();
      manual.scrollIntoView({block: "nearest"});
    }
  });
  controls.append(copy, feedback);
  content.append(manual);
}

function renderPrompt(view) {
  const prompt = view.prompt;
  const key = prompt ? "prompt:" + prompt.id : view.run + ":" + view.status + ":" + view.phaseTitle;
  if (panelKey === key) return;
  panelKey = key;
  resetCountdown();
  promptActions.replaceChildren();
  const content = get("interaction");
  content.replaceChildren();
  const title = get("panel-title");
  if (!prompt) {
    title.textContent = view.status === "complete" ? (actions[view.action] || "Wizard") + " complete" :
      view.status === "failed" ? "The wizard stopped" :
      view.status === "cancelled" ? "Wizard stopped" :
      view.status === "stopping" ? "Stopping the current operation" : view.phaseTitle;
    if (view.error) content.append(node("p", view.error, "problem"));
    if (completed(view.status)) {
      const back = button("Back to main page", "primary");
      back.addEventListener("click", async () => {
        back.disabled = true;
        try {
          const response = await post("/api/actions", {revision: view.revision});
          if (!response.ok && response.status !== 409) throw new Error("Couldn’t return to the action menu. Try again.");
          await refresh();
        } catch (error) {
          showConnection(error.message);
          back.disabled = false;
        }
      });
      promptActions.append(back);
      if (view.diagnostics) addCopyDiagnostics(promptActions, content, view.diagnostics);
    }
    if (view.status === "stopping") content.append(node("p", "Waiting for the operation to exit and release its locks."));
    return;
  }
  title.textContent = prompt.label;
  if (prompt.message) content.append(node("p", prompt.message, prompt.kind === "form" ? "problem" : ""));
  if (prompt.kind === "actions") {
    renderActions(content, prompt);
  } else if (prompt.kind === "form") {
    renderSettings(content, prompt);
  } else if (prompt.kind === "confirm") {
    for (const option of prompt.options) {
      const control = button(option.label, option.value === "yes" ? "primary" : "outline");
      control.addEventListener("click", () => answer(prompt, option.value));
      promptActions.append(control);
    }
  } else {
    renderSingleInput(content, prompt);
  }
  startCountdown(prompt);
}

function renderSignIn(text) {
  if (authText === text) return;
  authText = text;
  get("sign-in").hidden = !text;
  const links = get("sign-in-actions");
  links.replaceChildren();
  get("sign-in-output").textContent = clean(text);
  const urls = clean(text).match(/https:\/\/[^\s<>"']+/g) || [];
  const providerLinks = new Map();
  for (const candidate of urls) {
    try {
      const url = new URL(candidate.replace(/[).,]+$/, ""));
      const host = url.hostname;
      if (host.endsWith(".awsapps.com") || host.endsWith(".amazonaws.com") ||
          (/^[a-z0-9-]+\.signin\.aws$/.test(host) && url.pathname === "/platform/authorize")) {
        // AWS CLI lists the device URL that fills in the code last.
        providerLinks.set("AWS", url);
      } else if (host === "github.com" && url.pathname === "/login/device") {
        providerLinks.set("GitHub", url);
      }
    } catch {
      // Provider output is still available as text.
    }
  }
  for (const [provider, url] of providerLinks) {
    const link = node("a", `Open ${provider} sign-in`, "button primary");
    link.href = url.href;
    link.target = "_blank";
    link.rel = "noopener noreferrer";
    links.append(link);
  }
}

const iconPaths = {
  stack: "M12 2 2 7l10 5 10-5-10-5ZM2 12l10 5 10-5M2 17l10 5 10-5",
  database: "M20 6c0 2.2-3.6 4-8 4S4 8.2 4 6m16 0c0-2.2-3.6-4-8-4S4 3.8 4 6m16 0v12c0 2.2-3.6 4-8 4s-8-1.8-8-4V6m16 6c0 2.2-3.6 4-8 4s-8-1.8-8-4",
  key: "M21 3a5 5 0 0 0-6.2 7.8L3 22v-5l9.9-9.9A5 5 0 0 1 21 3Zm-2 3h.01",
  shield: "M12 22c-5-2-8-5.5-8-10V5l8-3 8 3v7c0 4.5-3 8-8 10Zm-4-10 3 3 5-5",
  route: "M3 5h18v14H3zM3 9h18m-14 5 3 2-3 2m6 0h4",
  terminal: "M3 5h18v14H3zm4 5 3 3-3 3m6 0h4",
  globe: "M22 12a10 10 0 1 0-20 0 10 10 0 0 0 20 0ZM2 12h20M12 2c3 3 4 6 4 10s-1 7-4 10c-3-3-4-6-4-10s1-7 4-10Z",
  mail: "M3 5h18v14H3zm0 1 9 7 9-7",
  activity: "M2 12h4l3-9 5 18 3-9h5",
  identity: "M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2m12-14a4 4 0 1 0-8 0 4 4 0 0 0 8 0Z",
  certificate: "M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9Zm0 0v6h6M8 15l2 2 4-4",
};

function icon(name) {
  const ns = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(ns, "svg");
  const path = document.createElementNS(ns, "path");
  svg.setAttribute("viewBox", "0 0 24 24");
  svg.setAttribute("aria-hidden", "true");
  svg.setAttribute("focusable", "false");
  path.setAttribute("d", iconPaths[name] || iconPaths.stack);
  svg.append(path);
  return svg;
}

const resourceGroups = {
  acm: ["AWS Certificate Manager", "certificate"],
  apigatewayv2: ["Amazon API Gateway", "route"],
  apigateway: ["Amazon API Gateway", "route"],
  cloudflare: ["Cloudflare DNS", "globe"],
  cloudwatch: ["Amazon CloudWatch and EventBridge", "activity"],
  cognito: ["Amazon Cognito", "identity"],
  dynamodb: ["Amazon DynamoDB", "database"],
  iam: ["AWS IAM", "shield"],
  kms: ["AWS KMS", "key"],
  lambda: ["AWS Lambda", "terminal"],
  pulumi: ["Pulumi components", "stack"],
  random: ["Generated values", "key"],
  route53: ["Amazon Route 53", "globe"],
  s3: ["Amazon S3", "database"],
  secretsmanager: ["AWS Secrets Manager", "key"],
  ses: ["Amazon SES", "mail"],
  sesv2: ["Amazon SES", "mail"],
};

const resourceTypes = {
  "acm:Certificate": ["TLS certificate", "Secures the HTTPS endpoint."],
  "acm:CertificateValidation": ["Certificate validation", "Verifies domain ownership for the certificate."],
  "apigatewayv2:Api": ["API", "Receives requests to the application."],
  "apigatewayv2:ApiMapping": ["API mapping", "Connects a custom domain to an API stage."],
  "apigatewayv2:Authorizer": ["Authorizer", "Checks whether requests can access the API."],
  "apigatewayv2:DomainName": ["API custom domain", "Gives the API its public hostname."],
  "apigatewayv2:Integration": ["API integration", "Connects API requests to the backend."],
  "apigatewayv2:Route": ["API route", "Maps an HTTP method and path to an integration."],
  "apigatewayv2:Stage": ["API stage", "Serves a deployment of the API."],
  "cloudflare:DnsRecord": ["DNS record", "Publishes a hostname or certificate validation record."],
  "cloudwatch:EventRule": ["EventBridge rule", "Defines when to invoke the target."],
  "cloudwatch:EventTarget": ["EventBridge target", "Sends scheduled or matching events to a service."],
  "cloudwatch:LogGroup": ["Log group", "Collects application logs."],
  "cognito:UserPool": ["User pool", "Manages users and signs authentication tokens."],
  "cognito:UserPoolClient": ["App client", "Configures how the app signs in to the user pool."],
  "dynamodb:Table": ["DynamoDB table", "Stores application records."],
  "iam:Role": ["IAM role", "Defines an identity an AWS service or workload can assume."],
  "iam:RolePolicy": ["IAM role policy", "Defines what the role can do."],
  "kms:Key": ["KMS key", "Manages a key used for encryption or signing."],
  "lambda:Function": ["Lambda function", "Runs backend code in response to an invocation."],
  "lambda:Permission": ["Invocation permission", "Authorizes a source to invoke a Lambda function."],
  "pulumi:Stack": ["Pulumi stack", "Tracks the resources for this deployment."],
  "random:RandomBytes": ["Random bytes", "Generates bytes for the application."],
  "route53:Record": ["DNS record", "Publishes a hostname or certificate validation record."],
  "s3:Bucket": ["S3 bucket", "Stores objects."],
  "s3:BucketV2": ["S3 bucket", "Stores objects."],
  "secretsmanager:Secret": ["Secret", "Stores protected application configuration."],
  "secretsmanager:SecretVersion": ["Secret version", "Stores a version of the secret value."],
  "ses:EmailIdentity": ["Email identity", "Verifies an email address for sending."],
  "apexfission:StorageAndSigner": ["Verification storage and signer", "Groups the verification table and signing key."],
  "apexfission:TestHost": ["Email verification test host", "Groups the resources used by the demo."],
};

function words(value) {
  return value.replace(/([A-Z])([A-Z][a-z])/g, "$1 $2")
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2").replace(/[-_]/g, " ");
}

function describeResource(resource) {
  const parts = resource.split("::");
  if (!parts[0].startsWith("urn:pulumi:") || parts.length < 4) {
    return {name: resource, type: "Resource", group: "Other resources", symbol: "stack"};
  }
  const [provider, module, kind] = parts[2].split("$").at(-1).split(":");
  const service = provider === "aws" ? module?.split("/")[0] : provider;
  const [group, symbol] = resourceGroups[service] || (provider === "aws" ?
    ["AWS " + words(service || "resources"), "stack"] : resourceGroups.pulumi);
  const [type, purpose] = resourceTypes[service + ":" + kind] || [words(kind || "Resource"), ""];
  return {name: parts.slice(3).join("::"), type, purpose, group, symbol};
}

function renderPlan(view) {
  const preview = get("preview");
  preview.hidden = !view.reviewed;
  if (!view.reviewed || planRun === view.run) return;
  planRun = view.run;
  const container = get("preview-changes");
  container.replaceChildren();
  const changes = view.changes || [];
  const count = view.total;
  get("preview-count").textContent = count === 0 ? "No resource operations planned." :
    count + (count === 1 ? " resource operation" : " resource operations") +
    (changes.length < count ? " · Showing the first " + changes.length : "");
  const groups = new Map();
  for (const {operation, resource} of changes) {
    const {name, type, purpose, group, symbol} = describeResource(resource);
    if (!groups.has(group)) {
      const section = node("section", "", "artifact-group");
      const heading = node("h3", group);
      const list = node("ul");
      section.append(heading, list);
      groups.set(group, list);
      container.append(section);
    }
    const item = node("li", "", "artifact");
    const picture = node("span", "", "artifact-icon");
    picture.append(icon(symbol));
    const details = node("details");
    const summary = node("summary", name);
    const urn = node("code", resource, "artifact-urn");
    details.append(summary, urn);
    const description = node("div", "", "artifact-info");
    description.append(node("strong", type), details);
    if (purpose) description.append(node("p", purpose));
    const label = words(operation);
    const badge = node("span", label.charAt(0).toUpperCase() + label.slice(1), "operation");
    badge.dataset.operation = operation;
    item.append(picture, description, badge);
    groups.get(group).append(item);
  }
}

function render(view) {
  current = view;
  get("project").textContent = view.title;
  get("action").textContent = actions[view.action] || "Deployment wizard";
  footer.hidden = false;
  stopControl.textContent = canClose(view) ? "Close wizard" : "Stop wizard";
  stopControl.disabled = view.status === "stopping";
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
    step.classList.toggle("passed", visited && (index < active || view.status === "complete"));
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
    resetCountdown();
    footer.hidden = true;
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
    renderVaultLock(Boolean(view.locked));
    if (!view.locked) render(view);
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

stopControl.addEventListener("click", async () => {
  const {run, phase, status} = current;
  const close = canClose(current);
  if (phase === "apply" && status === "running" &&
      !confirm("Stop this operation? Changes already sent to the cloud may still finish.")) return;
  try {
    const response = await post("/api/stop", {run, close});
    if (response.status === 409) {
      await refresh();
      return;
    }
    if (!response.ok) throw new Error("The wizard couldn’t stop. Check the terminal.");
    if (close) {
      closed = true;
      sessionStorage.removeItem(sessionKey);
      resetCountdown();
      footer.hidden = true;
      showConnection("");
      get("interaction").replaceChildren(node("p", "Local session closed. You can close this tab."));
    } else {
      await refresh();
    }
  } catch (error) {
    showConnection(error.message);
  }
});
poll();
