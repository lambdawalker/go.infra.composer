"use strict";

(() => {
  const labels = {
    memorySize: "Memory",
    timeout: "Timeout",
    reservedConcurrentExecutions: "Reserved concurrency",
    functionName: "Function name",
    hashKey: "Partition key",
    rangeKey: "Sort key",
    billingMode: "Billing mode",
    customerMasterKeySpec: "Key spec",
    keySpec: "Key spec",
    keyUsage: "Key usage",
  };

  function label(name) {
    return labels[name] || name.replace(/([a-z0-9])([A-Z])/g, "$1 $2").replace(/^./, letter => letter.toUpperCase());
  }

  function value(name, raw) {
    if (!raw) return "—";
    if (!/^-?\d+$/.test(raw)) return raw;
    if (name === "memorySize") return raw + " MB";
    if (name === "timeout") return raw + " s";
    return raw;
  }

  function append(parent, tag, text, className) {
    const child = document.createElement(tag);
    if (text) child.textContent = text;
    if (className) child.className = className;
    parent.append(child);
    return child;
  }

  window.renderWizardPreviewChange = (container, change) => {
    if (!change) return;
    const properties = change.properties || [];
    const replacementFields = change.replacementFields || [];
    const replacement = change.operation.includes("replace") || replacementFields.length > 0;
    if (!properties.length && !replacement) return;

    const section = append(container, "section", "", "artifact-preview-review");
    append(section, "h3", "Proposed configuration", "subartifact-heading");
    if (replacementFields.length) {
      append(section, "p", "Replacement triggered by " + replacementFields.map(label).join(", ") + ".");
    } else if (replacement) {
      append(section, "p", "Replacement field details are unavailable.", "muted");
    }
    if (!properties.length) return;

    const table = append(section, "table", "", "feature-table artifact-preview-values");
    table.setAttribute("aria-label", "Before and after values");
    const headers = append(append(table, "thead"), "tr");
    for (const title of ["Setting", "Before", "After"]) append(headers, "th", title).scope = "col";
    const rows = append(table, "tbody");
    for (const property of properties) {
      const row = append(rows, "tr");
      append(row, "th", label(property.name)).scope = "row";
      append(row, "td", value(property.name, property.before));
      append(row, "td", value(property.name, property.after));
    }
  };
})();
