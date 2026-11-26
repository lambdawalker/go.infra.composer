"use strict";

(() => {
  let rendered;
  const applyStatus = {
    "not-started": "Apply did not start",
    failed: "Apply failed. Some changes may have reached AWS.",
    succeeded: "Apply completed",
  };

  async function downloadReport(record, control, status) {
    control.disabled = true;
    status.textContent = "";
    try {
      const path = "/api/deployment-history/report?id=" + encodeURIComponent(record.id);
      const response = await fetch(path, {headers: authHeaders});
      if (!response.ok) throw new Error("Report unavailable");
      const reportURL = URL.createObjectURL(await response.blob());
      const link = node("a");
      link.href = reportURL;
      link.download = "deployment-" + record.id + ".html";
      link.click();
      setTimeout(() => URL.revokeObjectURL(reportURL), 30000);
    } catch {
      status.textContent = "Could not download the artifact report.";
    } finally {
      control.disabled = false;
    }
  }

  function changesText(record) {
    const counts = [
      [record.created, "create"], [record.updated, "update"],
      [record.replaced, "replace"], [record.deleted, "delete"], [record.other, "other"],
    ].filter(([count]) => count > 0);
    return counts.length ? "Plan changes · " + counts.map(([count, label]) => count + " " + label).join(" · ") :
      "No plan changes recorded.";
  }

  function historyItem(record) {
    const failed = record.outcome === "failed";
    const card = node("article", "", "deployment-history-entry");
    card.append(node("h3", failed ? "Latest failed attempt" : "Last successful deployment"));
    const date = node("time", new Date(record.finishedAt).toLocaleString());
    date.dateTime = record.finishedAt;
    card.append(date);
    if (record.revision) {
      const revision = node("p", "Commit ");
      const code = node("code", record.revision.slice(0, 12));
      code.title = record.revision;
      revision.append(code);
      if (record.dirty) revision.append(" · Working tree had changes");
      card.append(revision);
    }
    card.append(node("p", changesText(record)));
    if (failed || record.outcome === "incomplete") {
      let status = applyStatus[record.applyStatus] || "Apply status unavailable for this report.";
      if (record.outcome === "incomplete") status += " · Report incomplete";
      card.append(node("p", status));
    }
    const download = button("Download artifact report", "outline");
    download.setAttribute("aria-label", download.textContent + " for " + (failed ? "latest failed attempt" : "last successful deployment"));
    const feedback = node("p");
    feedback.setAttribute("role", "status");
    download.onclick = () => downloadReport(record, download, feedback);
    card.append(download, feedback);
    return card;
  }

  window.renderDeploymentHistory = view => {
    const history = get("deployment-history");
    const records = view.deploymentHistory || [];
    const environment = view.environment || view.inventory?.environment;
    const key = JSON.stringify([environment, records]);
    if (key === rendered) return;
    rendered = key;
    history.replaceChildren(node("h2", "Deployment history"));
    if (records.length === 0) {
      history.append(node("p", environment ? "No deployment reports for this environment yet." :
        "Select an environment to view deployment history.", "muted"));
      return;
    }
    const list = node("div", "", "deployment-history-list");
    list.append(...records.map(historyItem));
    history.append(list);
  };
})();
