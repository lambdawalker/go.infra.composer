"use strict";
(() => {
  function appendLinks(container, heading, artifacts, emptyMessage, navigate) {
    const title = document.createElement("h3");
    title.className = "subartifact-heading";
    title.textContent = heading;
    container.append(title);
    if (artifacts.length === 0) {
      const empty = document.createElement("p");
      empty.className = "muted";
      empty.textContent = emptyMessage;
      container.append(empty);
      return;
    }
    const list = document.createElement("ul");
    list.className = "detail-tree";
    list.setAttribute("aria-label", heading);
    for (const artifact of artifacts) {
      const item = document.createElement("li");
      const button = document.createElement("button");
      button.type = "button";
      button.className = "detail-tree-entry";
      button.textContent = artifact.name || artifact.key;
      button.onclick = () => navigate(artifact);
      item.append(button);
      list.append(item);
    }
    container.append(list);
  }

  window.renderArtifactDependencies = (container, selected, nodes, navigate) => {
    const links = new Set(selected.dependencies || []);
    const dependencies = [];
    const users = [];
    for (const artifact of nodes) {
      if (artifact.key === selected.key) continue;
      if (links.has(artifact.key)) dependencies.push(artifact);
      if (artifact.dependencies?.includes(selected.key)) users.push(artifact);
    }
    appendLinks(container, "Depends on", dependencies, "No dependencies recorded.", navigate);
    appendLinks(container, "Used by", users, "No dependents recorded.", navigate);
  };
})();
