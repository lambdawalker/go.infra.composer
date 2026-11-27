(() => {
  const $ = id => document.getElementById(id);
  const scheme = matchMedia("(prefers-color-scheme: light)");
  function theme() {
    document.documentElement.dataset.theme = $("report-theme").value || (scheme.matches ? "light" : "dark");
  }
  scheme.addEventListener("change", theme);
  $("report-theme").onchange = theme;
  theme();

  const cards = new Map(Array.from(document.querySelectorAll("details.artifact"), card => [card.dataset.key, card]));
  const artwork = JSON.parse($("artwork").textContent);
  let topology, selected, view;
  const nodes = [];
  for (const [key, card] of cards) {
    const {name, type, component, action} = card.dataset;
    const dependencies = JSON.parse(card.dataset.dependencies || "[]");
    nodes.push({key, name, type, dependencies, component:component === "true", action, parent:card.parentElement.closest("details.artifact")?.dataset.key});
    const summary = card.querySelector("summary");
    const icon = component === "true" ? "/icons/pulumi/component.svg" : window.wizardAWSIconFor(type);
    if (artwork[icon]) {
      const image = new Image();
      image.src = "data:image/svg+xml;base64," + artwork[icon];
      image.className = "icon"; image.alt = "";
      summary.querySelector("svg").replaceWith(image);
    }
    summary.addEventListener("click", () => select(key));
  }
  function select(key) {
    const card = cards.get(key);
    if (!card) return;
    cards.get(selected)?.removeAttribute("data-selected");
    selected = key;
    card.dataset.selected = "true";
    for (let parent = card.parentElement.closest("details.artifact"); parent; parent = parent.parentElement.closest("details.artifact")) parent.open = true;
    $("selected-name").textContent = card.dataset.name;
    const body = card.querySelector(".artifact-body");
    $("selected-fields").replaceChildren(body.querySelector(".owner-full").cloneNode(true), body.querySelector("dl").cloneNode(true));
    $("inspector").hidden = false;
    topology?.setSelection(key);
  }
  function openView(target) {
    const returningToTree = view === "topology" && target === "inventory";
    view = target;
    for (const button of $("views").children) {
      const active = button.dataset.view === view;
      button.setAttribute("aria-pressed", String(active));
      $(button.dataset.view + "-panel").hidden = !active;
    }
    $("inspector").hidden = !selected || view === "summary";
    if (returningToTree && selected) {
      const card = cards.get(selected);
      if (card.hidden) $("reset").click();
      card.open = true;
      card.querySelector("summary").focus();
    }
    if (view !== "topology") { topology?.deactivate(); return; }
    if (!topology) {
      const element = suffix => $("topology-" + suffix);
      topology = window.createArtifactTopology({
        stage:element("stage"), canvas:element("canvas"), tooltip:element("tooltip"), message:element("message"),
        selectedLabel:element("selected"), list:element("list"), search:element("search"), count:element("count"),
        fit:element("fit"), frameButton:element("frame"), branchButton:element("branch"), labels:element("labels"),
        iconSettings:element("icon-settings"), iconDialog:element("icon-dialog"), iconRows:element("icon-rows"),
        arrows:element("arrows"), focus:element("focus"), platformOpacity:element("opacity"), glassTransparency:element("glass"),
        changesOnly:element("changes"), trace:element("trace"), relationship:element("relationship"),
        navigation:element("navigation"), axes:element("axes"), box:element("box"), onSelect:select,
        async loadGLTF(loader, path) {
          if (!artwork[path]) throw Error("Missing snapshot model");
          const bytes = Uint8Array.from(atob(artwork[path]), char => char.charCodeAt(0));
          return loader.parseAsync(bytes.buffer, "");
        }
      });
      topology.setArtifacts(nodes);
      topology.setSelection(selected);
    }
    topology.activate();
  }
  for (const button of $("views").children) button.onclick = () => openView(button.dataset.view);
  for (const button of document.querySelectorAll(".metric")) button.onclick = () => {
    $("reset").click();
    const filter = $("filter-status");
    filter.value = Array.from(filter.options).find(option => option.textContent === button.dataset.status).value;
    filter.dispatchEvent(new Event("change"));
    for (const card of cards.values()) {
      if (card.dataset.status === button.dataset.status) {
        for (let parent = card.parentElement.closest("details.artifact"); parent; parent = parent.parentElement.closest("details.artifact")) parent.open = true;
      }
    }
    openView("inventory");
  };
  $("views").hidden = false;
  openView("summary");
})();
