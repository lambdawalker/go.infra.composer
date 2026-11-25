"use strict";

const assert = require("node:assert/strict");
const test = require("node:test");
const {categoryFor,layoutArtifacts,perspective,lookAt,multiply,project} = require("./topology.js");

test("classifies representative AWS resources into distinct primitives", () => {
  assert.equal(categoryFor("aws:lambda/function:Function"),"compute");
  assert.equal(categoryFor("aws:dynamodb/table:Table"),"storage");
  assert.equal(categoryFor("aws:apigatewayv2/domainName:DomainName"),"network");
  assert.equal(categoryFor("aws:apigatewayv2/authorizer:Authorizer"),"security");
  assert.equal(categoryFor("aws:kms/key:Key"),"security");
  assert.equal(categoryFor("aws:lambda/permission:Permission"),"security");
  assert.equal(categoryFor("aws:ses/emailIdentity:EmailIdentity"),"message");
  assert.equal(categoryFor("aws:cloudwatch/eventRule:EventRule"),"message");
});

test("lays out every artifact, retaining hierarchy and surviving malformed cycles", () => {
  const artifacts=[
    {key:"root",name:"root"},
    {key:"child",parent:"root",name:"child"},
    {key:"grandchild",parent:"child",name:"grandchild"},
    {key:"orphan",parent:"missing",name:"orphan"},
    {key:"cycle-a",parent:"cycle-b"},
    {key:"cycle-b",parent:"cycle-a"}
  ];
  const nodes=layoutArtifacts(artifacts),byKey=new Map(nodes.map(a=>[a.key,a]));
  assert.equal(nodes.length,artifacts.length);
  for(const node of nodes)assert.equal(node.position.length,3,node.key);
  assert.ok(byKey.get("child").position[1]>byKey.get("root").position[1]);
  assert.ok(byKey.get("grandchild").position[1]>byKey.get("child").position[1]);
  assert.notDeepEqual(byKey.get("orphan").position,byKey.get("root").position);
});

test("projects the camera target to canvas center for picking", () => {
  const camera=multiply(perspective(2),lookAt([0,4,15],[0,0,0]));
  const point=project([0,0,0],camera,800,400);
  assert.ok(point.visible);
  assert.ok(Math.abs(point.x-400)<.01);
  assert.ok(Math.abs(point.y-200)<.01);
});
