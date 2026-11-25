"use strict";
// The preview workspace consumes only the safe session inventory and saved plan.
(() => {
  const $ = id => document.getElementById(id);
  const el = (tag, text, cls) => { const n = document.createElement(tag); n.textContent = text || ""; if (cls) n.className = cls; return n; };
  const icon = symbol => { const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg"); svg.classList.add("icon"); svg.setAttribute("aria-hidden", "true"); const use = document.createElementNS(svg.namespaceURI, "use"); use.setAttribute("href", "#ws-" + symbol); svg.append(use); return svg; };
  const operation = value => (value || "unknown").toLowerCase();
  const badge = action => el("span", action === "same" ? "Unchanged" : action === "unknown" ? "Not planned" : action.replaceAll("-", " "), "badge " + (action.includes("delete") || action.includes("replace") ? "delete" : action.includes("create") ? "create" : action.includes("update") ? "update" : action === "unknown" ? "unknown" : "same"));
  const symbolFor = type => /route53|cloudflare|acm|domain|apiGateway/i.test(type) ? "globe" : /dynamodb|s3|storage/i.test(type) ? "database" : /iam|secret|cognito|kms|authorizer/i.test(type) ? "shield" : /ses|email/i.test(type) ? "mail" : /lambda|eventRule|eventTarget/i.test(type) ? "bolt" : "layers";
  const purposeFor = type => ({
    Function:"Runs backend code", Permission:"Allows a service to invoke the Lambda function",
    Table:"Stores application data", Bucket:"Stores deployment files or state",
    Key:"Signs or encrypts data", RandomBytes:"Generates cryptographic key material",
    EmailIdentity:"Identifies the SES sender", Api:"Exposes the application API",
    Stage:"Publishes an API deployment", Route:"Connects a request path to the API",
    Integration:"Connects the API to its backend", ApiMapping:"Maps a custom domain to the API",
    DomainName:"Gives the API a custom HTTPS hostname", DnsRecord:"Publishes a DNS record",
    Certificate:"Secures the public endpoint", CertificateValidation:"Verifies ownership of the certificate domain",
    UserPool:"Manages demo identities", UserPoolClient:"Allows the demo client to use the user pool",
    Authorizer:"Checks authorization for API requests", Role:"Grants service access",
    RolePolicy:"Defines the service's AWS permissions", Secret:"Stores a protected configuration value",
    SecretVersion:"Sets the protected configuration value", LogGroup:"Stores diagnostic logs",
    EventRule:"Schedules or responds to background events", EventTarget:"Routes a background event to its handler"
  })[type.split(":").at(-1)] || "Deployment resource for this application";
  const state = { selected:null, expanded:new Set(), filterKey:null, detailOpen:true, progressOpen:true, deploymentView:"application", inventory:null, plan:[], nodes:[], roots:[], reportRun:null };
  const workspace = $("preview-workspace"), wizard = $("wizard"), slot = $("preview-interaction-slot"), panel = $("panel"), signIn = $("sign-in"), originalParent = panel.parentNode;
  const dialog = $("workspace-dialog"), dialogContent = $("dialog-content"), promptActions = $("prompt-actions"), signInActions = $("sign-in-actions"), countdown = $("countdown"), stop = $("stop"), lockPanel = document.querySelector("#vault-lock .lock-panel");
  const actionHome = promptActions.parentNode, signInActionHome = signInActions.parentNode, countdownHome = countdown.parentNode, stopHome = stop.parentNode, lockHome = $("vault-lock");
  let routeKey = null;
  dialog.addEventListener("cancel", event => event.preventDefault());
  function setDialog(mode) {
    if ((mode || "") === (dialog.dataset.mode || "") && (!mode || dialog.open)) return;
    if (dialog.open) dialog.close();
    lockHome.append(lockPanel);
    if (panel.parentNode === dialogContent) slot.prepend(panel);
    if (signIn.parentNode === dialogContent) slot.append(signIn);
    countdownHome.prepend(countdown);
    stopHome.prepend(stop);
    signInActionHome.append(signInActions);
    actionHome.append(promptActions);
    dialog.dataset.mode = mode || "";
    if (!mode) return;
    dialog.setAttribute("aria-labelledby", mode === "vault" ? "vault-lock-title" : mode === "signIn" ? "sign-in-title" : "panel-title");
    if (mode === "vault") {
      dialogContent.append(lockPanel);
    } else if (mode === "signIn") {
      dialogContent.append(signIn);
      $("dialog-actions").append(stop, signInActions);
    } else {
      dialogContent.append(panel);
      $("dialog-countdown").append(countdown);
      $("dialog-actions").append(stop, promptActions);
    }
    dialog.showModal();
    if (mode === "signIn") {signIn.tabIndex = -1;signIn.focus();}
  }
  window.setWizardDialog = setDialog;
  function showWorkspace(view) {
    const show = !view.closed;
    if (!show) setDialog(null);
    if (show && !workspace.contains(panel)) slot.prepend(panel);
    if (show && !workspace.contains(signIn)) slot.append(signIn);
    if (!show && signIn.parentNode !== originalParent) originalParent.prepend(signIn);
    if (!show && panel.parentNode !== originalParent) originalParent.insertBefore(panel, signIn);
    wizard.hidden = show; workspace.hidden = !show;
    $("brand-title").textContent = view.title || "Deployment wizard";
    $("brand-operation").textContent = view.phase === "preview" ? "Preview" : view.phase === "apply" ? "Apply" :
      ({deploy:"Deploy",preview:"Preview",report:"Artifact report",setup:"Setup",status:"Status"})[view.action] || "Choose action";
    document.title = (view.title || "Deployment wizard") + " · " + $("brand-operation").textContent;
    return show;
  }
  function buildInventory(view) {
    const inventory = view.inventory || {artifacts:[]};
    const resources = new Map();
    const nodes = (inventory.artifacts || []).map((raw, id) => ({...raw, id, children:[], parentNode:null, action:view.reviewed ? "same" : operation(raw.action), symbol:symbolFor(raw.type || "")}));
    for (const a of nodes) resources.set(a.key, a);
    for (const a of nodes) { const parent = resources.get(a.parent); if (parent && parent !== a) { a.parentNode = parent; parent.children.push(a); } }
    state.inventory = inventory;
    state.filterKey = null;
    state.nodes = nodes;
    state.roots = nodes.filter(a => !a.parentNode);
    state.plan = (view.changes || []).map(change => ({...change, node:resources.get(change.resource)}));
    for (const change of state.plan) if (!change.node) { // Some Pulumi plan entries are not managed artifacts in the report.
      const resource = change.resource, name = resource.split("::").at(-1);
      const type = resource.split("::").at(-2)?.split("$").at(-1) || "Resource";
      const a = {key:resource, name, type, id:nodes.length, parent:"", children:[], parentNode:null, owner:inventory.repository, feature:"", description:purposeFor(type), presence:"unknown", action:operation(change.operation), symbol:symbolFor(type)};
      nodes.push(a); state.roots.push(a); resources.set(resource,a); change.node = a;
    }
    // Prefer the saved plan's operation when its event log was not available.
    for (const change of state.plan) change.node.action = operation(change.operation);
    if (!nodes.includes(state.selected)) state.selected = state.plan[0]?.node || nodes[0] || null;
  }
  function details() {
    const box = $("detail-content"); box.replaceChildren();
    const a = state.selected;
    if (!a) { box.append(el("p", "No artifacts loaded. Connect to a deployment stack to check.", "muted")); return; }
    box.append(el("div", a.name || a.key, "detail-name"), el("div", a.type || "Resource", "kind"), el("p", a.description || purposeFor(a.type || ""), "artifact-description"));
    const tags = el("div", "", "detail-actions"); tags.append(badge(a.action), el("span", a.feature || state.inventory?.repository?.split("/").at(-1) || "Project", "depth-label")); box.append(tags);
    const dl = el("dl", "", "data-grid");
    for (const [label, value] of [["Presence", ({present:"Live check: present",recorded:"Recorded in Pulumi state",absent:"Live check: absent",unknown:"Not checked"})[a.presence] || "Not checked"], ["Pulumi identity",a.key], ["Owner",a.owner || state.inventory?.repository || "Unknown"]]) {
      dl.append(el("dt",label),el("dd",value));
    }
    box.append(dl);
    const count = a.children.reduce(function visit(n,c){return n + 1 + c.children.reduce(visit,0)},0);
    const heading = el("h3", "Subartifacts (" + count + ")"); heading.className="subartifact-heading"; box.append(heading);
    if (!a.children.length) box.append(el("p", "No subartifacts", "muted"));
    else { const list=el("ul","","detail-tree"); for (const child of a.children) { const item=el("li"); const button=el("button",child.name || child.key,"detail-tree-entry"); button.type="button"; button.onclick=()=>select(child); item.append(button);list.append(item); }box.append(list); }
  }
  function select(a, reveal=false) { state.selected=a; for(let p=a.parentNode;p;p=p.parentNode) state.expanded.add(p.key); if(reveal) toggleDetails(true); details(); tree(); changes(); }
  function matches(a,q,filter) { return (a.name || a.key).toLowerCase().includes(q) && (filter==="all" || filter==="changed" && a.action!=="same" && a.action!=="unknown" || filter==="unchanged" && a.action==="same" || filter==="unknown" && a.presence==="unknown"); }
  function tree() {
    const q=$("search").value.trim().toLowerCase(), filter=$("filter").value;
    const filterKey=q+"\0"+filter, filterChanged=state.filterKey!==filterKey;
    state.filterKey=filterKey;
    const matching=state.nodes.filter(a=>matches(a,q,filter)); $("count").textContent=matching.length+" of "+state.nodes.length;
    const included=new Set(); for(const a of matching) for(let p=a;p;p=p.parentNode) {included.add(p);if(filterChanged&&(q||filter!=="all")) state.expanded.add(p.key);}
    const box=$("list");box.replaceChildren(); if(!matching.length) {box.append(el("p",state.nodes.length ? "No artifacts match." : "No artifacts loaded. Connect to a deployment stack to check.","muted"));return;}
    const list=el("ul","","artifact-tree");
    function appendNode(parent,a) {
      const item=el("li","","tree-node"), row=el("div","","tree-row"), children=a.children.filter(c=>included.has(c));
      const expanded=children.length>0 && state.expanded.has(a.key);
      item.classList.toggle("expanded",expanded);
      if(children.length) {
        const toggle=el("button","","branch-toggle");toggle.type="button";toggle.id="artifact-toggle-"+a.id;
        toggle.setAttribute("aria-label",(expanded?"Collapse ":"Expand ")+(a.name || a.key));
        toggle.setAttribute("aria-expanded",String(expanded));
        if(expanded)toggle.setAttribute("aria-controls","artifact-children-"+a.id);
        const chevron=el("span","","chevron");chevron.setAttribute("aria-hidden","true");toggle.append(chevron);
        toggle.onclick=()=>{expanded?state.expanded.delete(a.key):state.expanded.add(a.key);tree();$(toggle.id)?.focus();};row.append(toggle);
      } else row.append(el("span","","branch-spacer"));
      const entry=el("button","","tree-entry");entry.type="button";entry.append(icon(a.symbol));const content=el("span","","tree-content");content.append(el("span",a.name || a.key,"artifact-name"));const meta=el("span","","tree-meta");meta.append(badge(a.action),el("span",a.feature || state.inventory?.repository?.split("/").at(-1) || "Project","tree-owner"));content.append(meta);entry.append(content);entry.setAttribute("aria-current",String(state.selected===a));entry.onclick=()=>select(a);row.append(entry);item.append(row);
      if(expanded) {const sub=el("ul");sub.id="artifact-children-"+a.id;for(const child of children)appendNode(sub,child);item.append(sub);}parent.append(item);
    }
    for(const a of state.roots)if(included.has(a))appendNode(list,a);box.append(list);
  }
  function changes() {
    const box=$("changes");box.replaceChildren();
    for(const change of state.plan) {const a=change.node, card=el("button","","change-row");card.type="button";card.setAttribute("aria-current",String(a===state.selected));const top=el("span","","change-top");top.append(icon(a.symbol),el("strong",a.name || a.key));const meta=el("span","","change-meta");meta.append(el("small",a.type),badge(operation(change.operation)));card.append(top,meta);card.onclick=()=>select(a,true);box.append(card);}
  }
  function context(view) {
    const i=state.inventory || {};
    $("deployment-context").textContent=i.stack || "";
    const app=$("application-view");app.replaceChildren();
    if (!view.inventory) {
      app.append(el("p", "Application details appear after the target is configured.", "muted"));
    } else {
      const table=el("table","","feature-table"),head=el("thead"),row=el("tr");
      for(const label of ["Application / Feature","Repository"]) row.append(el("th",label));
      head.append(row);table.append(head);
      const body=el("tbody"),tr=el("tr");
      tr.append(el("td",i.project || view.title),el("td",i.repository || "Unknown"));body.append(tr);
      for(const feature of [...new Set(state.nodes.map(a=>a.feature).filter(Boolean))]) {
        const row=el("tr");row.append(el("td",feature),el("td",i.repository || ""));body.append(row);
      }
      table.append(body);app.append(table);
    }
    const panel=$("deployment-details-view");panel.replaceChildren();const dl=el("dl");for(const [label,value] of [["Environment",i.environment],["Account",i.account],["Region",i.region],["Backend",i.backend],["Project",i.project],["Stack",i.stack],["Revision",i.revision]])if(value){dl.append(el("dt",label),el("dd",value));}panel.append(dl);
    const settings=$("settings-summary"),values=$("settings-values");
    settings.hidden=view.prompt?.kind==="form";
    values.replaceChildren();
    for(const [label,value] of [["Project",i.project],["Environment",i.environment],["AWS account",i.account],["AWS region",i.region],["S3 state URL",i.backend]])if(value){values.append(el("dt",label),el("dd",value));}
    $("settings-note").textContent=i.environment ?
      "Saved target values. You can edit the deployment inputs when the wizard opens its form." :
      "No saved target yet. Select an action to configure this deployment.";
    $("artifact-total").textContent=String(i.artifacts?.length || 0);
    $("change-total").textContent=view.reviewed ? String(view.total || 0) : "—";
    $("unknown-total").textContent=String((i.artifacts || []).filter(a=>a.presence==="unknown").length);
    $("plan-state").textContent=view.reviewed ? "Plan ready" : view.action === "report" ? "Artifact report" : ["preview","deploy"].includes(view.action) ? "Preparing plan" : (i.artifacts?.length ? "Recorded state" : "Awaiting inventory");
    $("changes").dataset.state = view.reviewed ? "ready" : "pending";
    $("preview-summary").hidden = false;
    $("proposed-changes").hidden = !["preview", "deploy", "report"].includes(view.action);
    $("inventory-source").textContent = ({
      repository:"Project manifest loaded. Choose a target to inspect its deployment.",
      "selected-target":"Target selected. Connect to check its recorded artifacts.",
      "saved-target":"Saved deployment target loaded. Connect to check its artifacts.",
      "saved-report":"Previously recorded artifacts are shown; their current presence has not been checked.",
      "pulumi-state":"Artifacts recorded in the selected Pulumi stack. This is not a live cloud check."
    })[i.source] || "Choose a target to inspect its recorded artifacts. Cloud presence has not been checked.";
    $("console-status").textContent=view.status === "complete" ? "Complete" : view.status === "failed" ? "Failed" : "In progress";
    const output=$("console-output"),atBottom=output.scrollHeight-output.scrollTop-output.clientHeight<35;
    const lines=view.activity || [];output.replaceChildren(...lines.map(line=>el("div",line,"log-line")));$("line-count").textContent=String(lines.length);if(atBottom)output.scrollTop=output.scrollHeight;
  }
  function toggleDetails(open) { state.detailOpen=open;const body=$("detail-content");body.hidden=!open;const pane=$("right"),toggle=$("toggle-details"),stripe=$("stripe-details");toggle.textContent=open?"−":"+";for(const b of [toggle,stripe]){b.setAttribute("aria-expanded",String(open));b.setAttribute("aria-label",(open?"Collapse":"Expand")+" artifact details");}pane.dataset.detailsCollapsed=String(!open);pane.style.gridTemplateRows=open?(pane.dataset.openRows||""):"36px 0 minmax(225px,1fr)";stripe.classList.toggle("active",open); }
  function toggleProgress(open) {state.progressOpen=open;const pane=$("sidebar"),toggle=$("toggle-progress"),stripe=$("stripe-progress");$("progress-body").hidden=!open;toggle.textContent=open?"−":"+";for(const b of [toggle,stripe]){b.setAttribute("aria-expanded",String(open));b.setAttribute("aria-label",(open?"Collapse":"Expand")+" progress");}pane.dataset.progressCollapsed=String(!open);pane.style.gridTemplateRows=open?(pane.dataset.openRows||""):"36px 0 minmax(225px,1fr)";stripe.classList.toggle("active",open); }
  $("toggle-details").onclick=()=>toggleDetails(!state.detailOpen);$("stripe-details").onclick=()=>toggleDetails(!state.detailOpen);
  $("toggle-progress").onclick=()=>toggleProgress(!state.progressOpen);$("stripe-progress").onclick=()=>toggleProgress(!state.progressOpen);
  function setDeployment(which) {state.deploymentView=which;const app=which==="application";$("application-view").hidden=!app;$("deployment-details-view").hidden=app;$("deployment-title").textContent=app?"Application & features":"Deployment details";$("stripe-application").classList.toggle("active",app);$("stripe-deployment-details").classList.toggle("active",!app);$("stripe-application").setAttribute("aria-pressed",String(app));$("stripe-deployment-details").setAttribute("aria-pressed",String(!app));}
  $("stripe-application").onclick=()=>setDeployment("application");$("stripe-deployment-details").onclick=()=>setDeployment("details");
  $("search").oninput=tree;$("filter").onchange=tree;
  const tabs=[$("overview-tab"),$("actions-tab"),$("inputs-tab"),$("console-tab")], tablist=workspace.querySelector(".tabs");
  function activate(tab,focus=false){for(const t of tabs){const selected=t===tab;t.setAttribute("aria-selected",String(selected));t.tabIndex=selected?0:-1;$(t.getAttribute("aria-controls")).hidden=!selected;}if(focus)tab.focus();}
  for(const tab of tabs) {tab.onclick=()=>activate(tab);tab.onkeydown=e=>{const order=[...tablist.querySelectorAll(".tab")],index=order.indexOf(tab);if(e.altKey&&e.shiftKey&&["ArrowLeft","ArrowRight"].includes(e.key)){e.preventDefault();const neighbor=order[index+(e.key==="ArrowLeft"?-1:1)];if(neighbor)tablist.insertBefore(tab,e.key==="ArrowLeft"?neighbor:neighbor.nextSibling);return;}const next=e.key==="ArrowRight"?order[(index+1)%order.length]:e.key==="ArrowLeft"?order[(index+order.length-1)%order.length]:e.key==="Home"?order[0]:e.key==="End"?order.at(-1):null;if(next){e.preventDefault();activate(next,true);}};}
  let tabDrag=null;tablist.addEventListener("pointerdown",e=>{const tab=e.target.closest(".tab");if(tab&&e.button===0)tabDrag={tab,x:e.clientX,y:e.clientY,id:e.pointerId,started:false};});window.addEventListener("pointermove",e=>{if(!tabDrag||e.pointerId!==tabDrag.id)return;if(!tabDrag.started&&Math.hypot(e.clientX-tabDrag.x,e.clientY-tabDrag.y)<6)return;tabDrag.started=true;tabDrag.tab.classList.add("dragging");e.preventDefault();const before=[...tablist.querySelectorAll(".tab")].filter(t=>t!==tabDrag.tab).find(t=>e.clientX<t.getBoundingClientRect().left+t.getBoundingClientRect().width/2);tablist.insertBefore(tabDrag.tab,before||null);});window.addEventListener("pointerup",e=>{if(tabDrag?.id===e.pointerId){tabDrag.tab.classList.remove("dragging");tabDrag=null;}});
  $("wrap-output").onclick=e=>{const pressed=e.currentTarget.getAttribute("aria-pressed")!=="true";e.currentTarget.setAttribute("aria-pressed",String(pressed));$("console-output").classList.toggle("wrapped",pressed);};
  const ws=$("workspace");let drag=null;const clamp=(n,min,max)=>Math.min(max,Math.max(min,n));
  for(const s of workspace.querySelectorAll('[role="separator"]')){
    s.onpointerdown=e=>{drag={s,x:e.clientX,y:e.clientY};s.setPointerCapture?.(e.pointerId);};
    s.onpointermove=e=>{if(drag?.s!==s)return;const side=s.dataset.side,dx=e.clientX-drag.x,dy=e.clientY-drag.y;if(side==="left"||side==="right"){const key=side==="left"?"--left":"--right",fallback=side==="left"?290:380,min=side==="left"?245:320,max=side==="left"?430:700;ws.style.setProperty(key,clamp((parseFloat(getComputedStyle(ws).getPropertyValue(key))||fallback)+(side==="left"?dx:-dx),min,max)+"px");}else{const pane=$(side==="sidebar-height"?"sidebar":"right"),minimum=side==="sidebar-height"?180:195;const first=parseFloat(getComputedStyle(pane).gridTemplateRows)||minimum;const next=clamp(first+dy,minimum,pane.getBoundingClientRect().height-225);pane.style.gridTemplateRows=next+"px 6px minmax(225px,1fr)";pane.dataset.openRows=pane.style.gridTemplateRows;const stripe=workspace.querySelector(side==="sidebar-height"?".left-stripe":".right-stripe");stripe.style.gridTemplateRows=pane.style.gridTemplateRows;}drag.x=e.clientX;drag.y=e.clientY;};
    s.onpointerup=()=>{drag=null;};s.onkeydown=e=>{if(["ArrowLeft","ArrowRight","ArrowUp","ArrowDown"].includes(e.key)){e.preventDefault();const horizontal=s.dataset.side==="left"||s.dataset.side==="right",sign=["ArrowRight","ArrowDown"].includes(e.key)?1:-1;const base=horizontal?{clientX:0,clientY:0}:{clientX:0,clientY:0};drag={s,x:0,y:0};s.onpointermove({...base,clientX:horizontal?sign*16:0,clientY:horizontal?0:sign*16});drag=null;}};
  }
  window.updatePreviewWorkspace = view => {
    if(!showWorkspace(view)) {state.reportRun=null;return;}
    const kind=view.prompt?.kind;
    const target=kind==="actions"?$("actions-slot"):kind==="form"?$("inputs-slot"):slot;
    const modal=kind && kind!=="actions" && kind!=="form" ? "prompt" :
      view.signIn ? "signIn" : null;
    setDialog(modal);
    if(!modal && panel.parentNode!==target) target.prepend(panel);
    // A running stage is already represented by its progress substep. Keep
    // the center panel for forms, questions, outcomes, and diagnostics.
    panel.hidden=!kind && Boolean(view.action) && ["running","stopping"].includes(view.status);
    $("actions-empty").hidden=kind==="actions";
    const nextRoute=view.run+":"+(view.prompt?.id||view.phase+":"+view.status);
    if(routeKey!==nextRoute) {
      routeKey=nextRoute;
      if(kind==="actions") activate($("actions-tab"));
      else if(kind==="form") activate($("inputs-tab"));
      else if(!modal && view.action) activate($("overview-tab"));
      if(modal) (modal==="signIn" ? signIn : dialog.querySelector("input:not([type=hidden])") || dialog.querySelector("button"))?.focus();
    }
    const key=view.run+":"+JSON.stringify(view.inventory)+":"+JSON.stringify(view.changes);
    if(key!==state.reportRun) {state.reportRun=key;buildInventory(view);details();tree();changes();}
    context(view);
    const source=[...$("phases").children];
    $("workspace-phases").replaceChildren(...source.filter(n=>!n.hidden).map(n=>{
      const li=el("li","",n.className),phase=n.dataset.phase;
      li.append(el("div",n.textContent,"phase-row"));
      const entries=(view.substeps||[]).filter(step=>step.phase===phase);
      if(!entries.length && view.action && view.phase===phase && view.phaseTitle) {
        entries.push({phase,title:view.phaseTitle,status:view.status});
      }
      if(entries.length) {
        li.classList.add("has-substeps");
        const steps=el("ol","","substeps");
        for(const step of entries) {
          const item=el("li","","substep "+(step.status||"running"));
          item.append(el("span","","substep-marker"),el("span",step.title,"substep-title"));
          if(step.status==="running" || step.status==="stopping")item.setAttribute("aria-current","step");
          steps.append(item);
        }
        li.append(steps);
      } else if(n.getAttribute("aria-current"))li.setAttribute("aria-current","step");
      return li;
    }));
  };
})();
