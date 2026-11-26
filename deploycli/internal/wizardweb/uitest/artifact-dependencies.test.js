"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const vm = require("node:vm");

function element(tag) {
  return {
    tag,
    children: [],
    attributes: {},
    append(child) { this.children.push(child); },
    setAttribute(name, value) { this.attributes[name] = value; },
  };
}

test("renders direct recorded dependencies and dependents as navigation targets", () => {
  const window = {};
  const document = {createElement: element};
  const code = fs.readFileSync(path.join(__dirname, "..", "ui", "artifact-dependencies.js"), "utf8");
  vm.runInNewContext(code, {window, document});

  const role = {key: "role", name: "Lambda role"};
  const fn = {key: "fn", name: "Host Lambda", dependencies: ["role", "role", "unknown", "fn"]};
  const permission = {key: "permission", name: "Invoke permission", dependencies: ["fn"]};
  const container = element("div");
  let selected;
  window.renderArtifactDependencies(container, fn, [role, fn, permission], target => { selected = target; });

  const [dependsOn, dependencyList, usedBy, userList] = container.children;
  assert.equal(dependsOn.textContent, "Depends on");
  assert.equal(usedBy.textContent, "Used by");
  assert.equal(dependencyList.children.length, 1);
  assert.equal(userList.children.length, 1);

  const dependencyButton = dependencyList.children[0].children[0];
  const userButton = userList.children[0].children[0];
  assert.equal(dependencyButton.textContent, role.name);
  assert.equal(userButton.textContent, permission.name);
  dependencyButton.onclick();
  assert.equal(selected, role);
  userButton.onclick();
  assert.equal(selected, permission);
});
