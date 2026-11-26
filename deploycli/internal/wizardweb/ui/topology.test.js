"use strict";

const assert = require("node:assert/strict");
const test = require("node:test");
const {categoryFor,layoutArtifacts,perspective,lookAt,multiply,project,modelPathFor,matchesArtifact,visibleEdges} = require("./topology.js");
const {awsIconFor} = require("../../../awsicons/aws-icons.js");

test("keeps fallback categories for resources without a service model", () => {
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


test("chooses the same AWS service for the 3D model and artifact icon", () => {
  const cases = {
    "aws:lambda/function:Function":"lambda", "aws:lambda/permission:Permission":"lambda",
    "aws:s3/stateBucket":"s3", "aws:s3:Bucket":"s3", "aws:s3/bucketObjectv2:BucketObjectv2":"s3",
    "aws:rds/instance:Instance":"rds", "aws:neptune/cluster:Cluster":"neptune",
    "aws:dynamodb/table:Table":"dynamodb", "aws:apigatewayv2/authorizer:Authorizer":"api-gateway",
    "aws:acm/certificate:Certificate":"acm", "aws:cloudwatch/logGroup:LogGroup":"cloudwatch",
    "aws:cloudwatch/eventRule:EventRule":"eventbridge", "aws:cloudwatch:EventTarget":"eventbridge",
    "aws:cloudwatch/eventTarget:EventTarget":"eventbridge",
    "aws:cognito/userPool:UserPool":"cognito", "aws:cognito/userPoolClient:UserPoolClient":"cognito",
    "aws:iam/role:Role":"iam", "aws:kms/key:Key":"kms",
    "aws:route53/record:Record":"route53", "aws:secretsmanager/secret:Secret":"secretsmanager",
    "aws:ses/emailIdentity:EmailIdentity":"ses"
  };
  for(const [type,service] of Object.entries(cases)) {
    assert.equal(modelPathFor(type,awsIconFor),`/models/aws/${service}.glb`,type);
  }
});

test("Cloudflare DNS records use the cloud signpost in details and topology", () => {
  for (const type of ["cloudflare:index/dnsRecord:DnsRecord", "cloudflare:DnsRecord", "cloudflare:index/record:Record"]) {
    assert.equal(awsIconFor(type), "/icons/cloudflare/dns.svg");
    assert.equal(modelPathFor(type, awsIconFor), "/models/cloudflare/dns.glb");
  }
});

test("uses basic shapes for unknown providers and never loads an external model", () => {
  for(const type of ["", "cloudflare:index/zone:Zone", "random:index/randomBytes:RandomBytes",
    "apexfission:email-verification:TestHost", "aws:unknown/resource:Resource"]) {
    assert.equal(modelPathFor(type,awsIconFor),"",type);
  }
  assert.equal(modelPathFor("aws:s3/stateBucket"),"");
  for(const source of ["https://example.com/icons/aws/s3.svg", "/icons/aws/../s3.svg", "/icons/aws/s3.svg?token=x"]) {
    assert.equal(modelPathFor("anything",()=>source),"");
  }
});

test("search shows matching artifacts and only edges whose endpoints remain visible", () => {
  const items=layoutArtifacts([
    {key:"root",name:"demo-role",type:"aws:iam/role:Role"},
    {key:"child",name:"demo-function",type:"aws:lambda/function:Function",parent:"root"},
    {key:"other",name:"state-bucket",type:"aws:s3/stateBucket",parent:"root"},
    {key:"orphan",name:"demo-orphan",type:"custom",parent:"missing"}
  ]);
  assert.deepEqual(visibleEdges(items,""),[["root","child"],["root","other"]]);
  assert.deepEqual(visibleEdges(items,"demo"),[["root","child"]]);
  assert.deepEqual(visibleEdges(items,"lambda"),[]);
  assert.equal(matchesArtifact(items[1],"lambda"),true);
  assert.equal(matchesArtifact(items[1],"state"),false);
  assert.equal(items.length,4,"filtering must not discard the complete inventory");
});

// Use the bundled Three scene graph/math, with only the GPU and model transport
// replaced. This exercises the real renderer lifecycle without requiring WebGL.
function topologyHarness({failRenderer=false}={}) {
  const fs=require("node:fs"),path=require("node:path"),vm=require("node:vm");
  class Element {
    constructor(tag="div") {this.tag=tag;this.children=[];this.dataset={};this.style={};this.value="";this.hidden=false;this.events=new Map();this.attributes={};this.classList={add(){},remove(){}};}
    addEventListener(event,fn){if(!this.events.has(event))this.events.set(event,[]);this.events.get(event).push(fn);}
    removeEventListener(event,fn){this.events.set(event,(this.events.get(event)||[]).filter(f=>f!==fn));}
    emit(event,detail={}){for(const fn of [...this.events.get(event)||[]])fn({preventDefault(){},...detail});}
    append(...children){this.children.push(...children);}
    replaceChildren(...children){this.children=children;}
    setAttribute(name,value){this.attributes[name]=value;}
    querySelectorAll(tag){return this.children.flatMap(child=>[...(child.tag===tag?[child]:[]),...child.querySelectorAll(tag)]);}
    getBoundingClientRect(){return {left:0,top:0,width:800,height:400};}
  }
  const frames=[],loads=new Map(),loadRequests=[],renders=[],stats={constructs:0,disposes:0,environmentDisposes:0};
  const document=new Element();document.hidden=false;document.documentElement=new Element();document.documentElement.dataset.theme="dark";
  document.createElement=tag=>new Element(tag);
  const context=new Element();Object.assign(context,{console,document,module:{exports:{}},devicePixelRatio:1,
    requestAnimationFrame:fn=>frames.push(fn),getComputedStyle:()=>({backgroundColor:"rgb(30,30,31)"}),
    ResizeObserver:class {observe(){}disconnect(){}},MutationObserver:class {observe(){}disconnect(){}}});
  context.window=context;
  vm.runInNewContext(fs.readFileSync(path.join(__dirname,"vendor/topology-three.js"),"utf8"),context);
  const T=context.WizardTopologyThree.THREE;
  context.WizardTopologyThree={THREE:{...T,
    WebGLRenderer:class {
      constructor(){stats.constructs++;if(failRenderer)throw Error("No WebGL");}
      setPixelRatio(){}setSize(){}render(scene,camera){renders.push({scene,camera});}dispose(){stats.disposes++;}
    },
    PMREMGenerator:class {fromScene(){return {texture:{},dispose(){stats.environmentDisposes++;}};}dispose(){}}
  },RoomEnvironment:class {dispose(){}},GLTFLoader:class {
    loadAsync(url){loadRequests.push(url);const record={};record.promise=new Promise((resolve,reject)=>Object.assign(record,{resolve,reject}));loads.set(url,record);return record.promise;}
  }};
  context.wizardAWSIconFor=awsIconFor;
  vm.runInNewContext(fs.readFileSync(path.join(__dirname,"topology.js"),"utf8"),context);
  const elements=Object.fromEntries(["stage","canvas","tooltip","message","selectedLabel","list","search","count","fit"].map(key=>[key,new Element()]));
  const selected=[];
  const topology=context.createArtifactTopology({...elements,onSelect:key=>selected.push(key)});
  function flush(){let calls=0;while(frames.length){assert.ok(++calls<5,"rendering must settle without an animation loop");frames.shift()();}}
  function model(){const scene=new T.Group(),geometry=new T.BoxGeometry(2,2,2),material=new T.MeshStandardMaterial({color:0x8844cc});
    material.name="catalog glass";material.transmission=.873;scene.add(new T.Mesh(geometry,material));return {scene,geometry,material};}
  function artifactNodes(){return renders.at(-1)?.scene.children.filter(child=>child.userData.artifactKey)||[];}
  return {topology,context,elements,frames,loads,loadRequests,renders,stats,flush,model,artifactNodes,selected};
}

const settleModels=()=>new Promise(resolve=>setImmediate(resolve));

test("renders on demand, pauses while hidden, and resumes after an empty inventory", async () => {
  const h=topologyHarness();
  h.topology.setArtifacts([{key:"bucket",name:"state",type:"aws:s3/stateBucket"},{key:"second",type:"aws:s3/bucket:Bucket"}]);
  assert.equal(h.stats.constructs,0,"inactive topology must not create a GPU context");
  h.topology.activate();h.flush();
  assert.equal(h.renders.length,1);
  assert.equal(h.loadRequests.length,1,"artifacts of the same service share one model download");
  h.topology.deactivate();
  const cube=h.model();h.loads.get("/models/aws/s3.glb").resolve(cube);
  await settleModels();
  assert.equal(h.frames.length,0,"a model completion must not render a hidden tab");
  h.topology.activate();h.flush();
  const node=h.artifactNodes()[0];
  let renderedMaterial;node.traverse(object=>{if(object.isMesh)renderedMaterial=object.material;});
  assert.equal(renderedMaterial,cube.material,"instances retain the catalog material");
  h.elements.search.value="missing";h.elements.search.emit("input");h.flush();
  assert.equal(node.visible,false);
  assert.equal(cube.material.transmission,.873);
  assert.equal(cube.material.opacity,1,"search must not mutate shared glass materials");
  h.elements.search.value="";h.elements.search.emit("input");
  h.topology.setArtifacts([]);h.flush();assert.equal(h.elements.canvas.hidden,true);
  h.topology.setArtifacts([{key:"bucket",type:"aws:s3/stateBucket"}]);await settleModels();h.flush();
  assert.equal(h.elements.canvas.hidden,false);
  assert.equal(h.artifactNodes().length,1);
  assert.equal(h.loadRequests.length,1,"reloaded inventory reuses the service template");
  h.topology.dispose();assert.equal(h.stats.disposes,1);assert.equal(h.stats.environmentDisposes,1);
  h.topology.activate();h.flush();assert.equal(h.stats.constructs,1,"disposed topology cannot restart");
});

test("ignores stale model completions and restores the current snapshot after context loss", async () => {
  const h=topologyHarness();
  h.topology.setArtifacts([{key:"old",type:"aws:s3/stateBucket"}]);h.topology.activate();h.flush();
  h.topology.setArtifacts([{key:"current",type:"aws:lambda/function:Function"}]);h.flush();
  h.loads.get("/models/aws/s3.glb").resolve(h.model());await settleModels();h.flush();
  assert.deepEqual(Array.from(h.artifactNodes(),node=>node.userData.artifactKey),["current"]);
  const cube=h.model();h.loads.get("/models/aws/lambda.glb").resolve(cube);await settleModels();h.flush();
  h.elements.canvas.emit("webglcontextlost");assert.equal(h.elements.canvas.hidden,true);
  h.topology.setSelection("current");assert.equal(h.frames.length,0);
  h.elements.canvas.emit("webglcontextrestored");await settleModels();h.flush();
  assert.equal(h.stats.constructs,2);assert.equal(h.stats.disposes,1);
  assert.equal(h.elements.canvas.hidden,false);
  assert.deepEqual(Array.from(h.artifactNodes(),node=>node.userData.artifactKey),["current"]);
  h.topology.dispose();assert.equal(h.stats.disposes,2);
});

test("keeps the accessible artifact list when WebGL or an individual model is unavailable", async () => {
  const missingGPU=topologyHarness({failRenderer:true});
  missingGPU.topology.setArtifacts([{key:"bucket",name:"state",type:"aws:s3/stateBucket"}]);
  missingGPU.topology.activate();missingGPU.flush();
  assert.equal(missingGPU.elements.canvas.hidden,true);
  assert.equal(missingGPU.elements.list.querySelectorAll("button").length,1);
  assert.match(missingGPU.elements.message.textContent,/3D view is unavailable/);
  missingGPU.topology.dispose();
  const h=topologyHarness();h.topology.setArtifacts([{key:"bucket",type:"aws:s3/stateBucket"}]);h.topology.activate();h.flush();
  h.loads.get("/models/aws/s3.glb").reject(Error("Missing asset"));await settleModels();h.flush();
  assert.equal(h.elements.canvas.hidden,false);
  assert.equal(h.artifactNodes()[0].children[0].isMesh,true,"missing models retain a basic shape");
  assert.match(h.elements.message.textContent,/basic shapes/);h.topology.dispose();
});


test("disposes a model that finishes loading after the topology is closed", async () => {
  const h=topologyHarness();h.topology.setArtifacts([{key:"late",type:"aws:s3/stateBucket"}]);h.topology.activate();h.flush();
  const cube=h.model();let geometries=0,materials=0;
  cube.geometry.addEventListener("dispose",()=>geometries++);cube.material.addEventListener("dispose",()=>materials++);
  h.topology.dispose();h.loads.get("/models/aws/s3.glb").resolve(cube);await settleModels();h.flush();
  assert.equal(geometries,1);assert.equal(materials,1);
  assert.equal(h.renders.length,1,"late loads must not restart rendering");
});

test("picks the artifact behind a loaded cube and does not select after orbit dragging", async () => {
  const h=topologyHarness();h.topology.setArtifacts([{key:"picked",type:"aws:s3/stateBucket"}]);h.topology.activate();h.flush();
  h.loads.get("/models/aws/s3.glb").resolve(h.model());await settleModels();h.flush();
  h.elements.stage.emit("pointerdown",{button:0,pointerId:1,clientX:400,clientY:200});
  h.elements.stage.emit("pointerup",{pointerId:1,clientX:400,clientY:200});
  assert.deepEqual(h.selected,["picked"]);
  h.elements.stage.emit("pointerdown",{button:0,pointerId:2,clientX:400,clientY:200});
  h.elements.stage.emit("pointermove",{pointerId:2,clientX:440,clientY:210});h.flush();
  h.elements.stage.emit("pointerup",{pointerId:2,clientX:440,clientY:210});
  assert.deepEqual(h.selected,["picked"]);h.topology.dispose();
});

test("uses an opaque floor beneath the inventory and updates its palette with the theme", () => {
  const h=topologyHarness();
  h.topology.setArtifacts([{key:"root",type:"custom"}]);h.topology.activate();h.flush();
  const stage=h.renders.at(-1).scene;
  const floor=stage.children.find(object=>object.geometry?.type==="PlaneGeometry");
  assert.ok(floor.position.y < -1 && !floor.material.transparent);
  const dark=floor.material.uniforms.surface.value.getHex();
  h.context.document.documentElement.dataset.theme="light";
  h.topology.fitView();h.flush();
  assert.notEqual(floor.material.uniforms.surface.value.getHex(),dark);
  let released=0;
  for(const resource of [floor.geometry,floor.material])resource.addEventListener("dispose",()=>released++);
  h.topology.dispose();assert.equal(released,2);
});
