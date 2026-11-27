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
  assert.ok(byKey.get("child").position[1]<byKey.get("root").position[1]);
  assert.ok(byKey.get("grandchild").position[1]<byKey.get("child").position[1]);
  assert.notDeepEqual(byKey.get("orphan").position,byKey.get("root").position);
});

test("component groups own their resources regardless of input order or provider name", () => {
  const nodes = layoutArtifacts([
    {key:"signing", type:"aws:kms/key:Key", parent:"backend"},
    {key:"backend", type:"example:verification:Backend", component:true},
    {key:"transactions", type:"aws:dynamodb/table:Table", parent:"backend"}
  ]);
  const group = nodes.find(item => item.key === "backend");
  assert.equal(group.category, "component");
  assert.deepEqual(group.children.map(item => item.key), ["signing","transactions"]);
  assert.deepEqual(visibleEdges(nodes,""), [["backend","signing"],["backend","transactions"]]);
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
    "aws:apigatewayv2/domainName:DomainName": "api-gateway-domain-name",
    "aws:apigatewayv2:DomainName": "api-gateway-domain-name",
    "aws:apigatewayv2/apiMapping:ApiMapping":"api-gateway-mapping",
    "aws:apigatewayv2/integration:Integration":"api-gateway-integration",
    "aws:apigatewayv2/route:Route":"api-gateway-route", "aws:apigatewayv2/api:Api":"api-gateway",
    "aws:lambda/function:Function":"lambda", "aws:lambda/permission:Permission":"lambda-permission",
    "aws:s3/stateBucket":"s3", "aws:s3:Bucket":"s3", "aws:s3/bucketObjectv2:BucketObjectv2":"s3",
    "aws:rds/instance:Instance":"rds", "aws:neptune/cluster:Cluster":"neptune",
    "aws:dynamodb/table:Table":"dynamodb", "aws:apigatewayv2/authorizer:Authorizer":"api-gateway",
    "aws:acm/certificate:Certificate":"acm", "aws:cloudwatch/logGroup:LogGroup":"cloudwatch",
    "aws:cloudwatch/eventRule:EventRule":"eventbridge", "aws:cloudwatch:EventTarget":"eventbridge-target",
    "aws:cloudwatch/eventTarget:EventTarget":"eventbridge-target",
    "aws:cognito/userPool:UserPool":"cognito", "aws:cognito/userPoolClient:UserPoolClient":"cognito-user-pool-client",
    "aws:apigatewayv2/stage:Stage":"api-gateway-stage",
    "aws:iam/role:Role":"iam-role", "aws:acm/certificateValidation:CertificateValidation":"acm-validation", "aws:kms/key:Key":"kms",
    "aws:route53/record:Record":"route53", "aws:secretsmanager/secret:Secret":"secretsmanager",
    "aws:secretsmanager/secretVersion:SecretVersion":"secret-version",
    "aws:ses/emailIdentity:EmailIdentity":"ses"
  };
  assert.equal(modelPathFor("aws:apigatewayv2/authorizer:Authorizer",awsIconFor,"jwt"), "/models/aws/api-gateway-jwt.glb");
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

test("uses the dice cup for Pulumi RandomBytes", () => {
  const type="random:index/randomBytes:RandomBytes";
  assert.equal(awsIconFor(type),"/icons/random/random-bytes.svg");
  assert.equal(modelPathFor(type,awsIconFor),"/models/random/random-bytes.glb");
});

test("uses basic shapes for unknown providers and never loads an external model", () => {
  for(const type of ["", "cloudflare:index/zone:Zone", "random:index/randomInteger:RandomInteger",
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
    contains(node){return node===this||this.children.some(child=>child.contains(node));}
    focus(){}
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
  context.WizardTopologyThree={...context.WizardTopologyThree,THREE:{...T,
    WebGLRenderer:class {
      constructor({canvas}){this.canvas=canvas;this.extensions={has:()=>true};this.capabilities={maxTextureSize:4096};this.shadowMap={};stats.constructs++;if(failRenderer)throw Error("No WebGL");}
      setDrawingBufferSize(w,h,r){this.canvas.width=Math.floor(w*r);this.canvas.height=Math.floor(h*r);}
      getDrawingBufferSize(v){return v.set(this.canvas.width,this.canvas.height);}
      getRenderTarget(){return this.target||null;} setRenderTarget(t){this.target=t;}
      getClearColor(c){return c.setHex(0);} getClearAlpha(){return 1;} setClearColor(){} clear(){}
      render(scene,camera){if(!this.target && scene.name!=="Selection outline overlay")renders.push({scene,camera});}dispose(){stats.disposes++;}
    },
    PMREMGenerator:class {fromScene(){return {texture:{},dispose(){stats.environmentDisposes++;}};}dispose(){}}
  },RoomEnvironment:class {dispose(){}},GLTFLoader:class {
    loadAsync(url){loadRequests.push(url);const record={};record.promise=new Promise((resolve,reject)=>Object.assign(record,{resolve,reject}));loads.set(url,record);return record.promise;}
  }};
  context.wizardAWSIconFor=awsIconFor;
  vm.runInNewContext(fs.readFileSync(path.join(__dirname,"topology.js"),"utf8"),context);
  const elements=Object.fromEntries(["stage","canvas","tooltip","message","selectedLabel","list","search","count","fit","tlsModel","arrows","navigation","axes","box"].map(key=>[key,new Element()]));
  elements.arrows.checked=true;
  elements.navigation.append(elements.axes);
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

test("supersamples the canvas and casts service and fallback shadows onto the floor", async () => {
  const h=topologyHarness();
  h.topology.setArtifacts([{key:"cert",type:"aws:acm/certificate:Certificate"}]);h.topology.activate();h.flush();
  assert.equal(h.elements.canvas.width,1600);
  assert.equal(h.elements.canvas.height,800);
  assert.equal(h.elements.tlsModel.hidden,false);
  assert.equal(h.artifactNodes()[0].children[0].castShadow,true);
  const scene=h.renders.at(-1).scene;
  assert.ok(scene.children.some(o=>o.isDirectionalLight && o.castShadow));
  assert.ok(scene.children.some(o=>o.geometry?.type==="PlaneGeometry" && o.receiveShadow));
  h.loads.get("/models/aws/acm.glb").resolve(h.model());await settleModels();h.flush();
  h.artifactNodes()[0].traverse(o=>{if(o.isMesh)assert.ok(o.castShadow);});
  h.elements.tlsModel.value="shield";h.elements.tlsModel.emit("change");
  assert.ok(h.loads.has("/models/aws/acm-shield.glb"));
  h.topology.dispose();
});

test("middle-drag translates in the camera plane and Home restores the opening camera", () => {
  const h=topologyHarness();
  h.topology.setArtifacts([{key:"root",type:"custom"},{key:"child",parent:"root",type:"custom"}]);
  h.topology.activate();h.flush();
  const {camera}=h.renders.at(-1),stage=h.elements.stage;
  const start=camera.position.clone(),orientation=camera.quaternion.clone();
  stage.emit("pointerdown",{button:1,pointerId:1,clientX:400,clientY:200});
  stage.emit("pointermove",{pointerId:1,clientX:480,clientY:230});
  stage.emit("pointerup",{pointerId:1,clientX:480,clientY:230});h.flush();
  assert.ok(camera.position.distanceTo(start)>1);
  assert.ok(orientation.angleTo(camera.quaternion)<1e-6);
  assert.deepEqual(h.selected,[]);
  stage.emit("keydown",{key:"ArrowLeft"});stage.emit("keydown",{key:"+"});h.flush();
  stage.emit("keydown",{key:"Home"});h.flush();
  assert.ok(camera.position.distanceTo(start)<1e-8);
  assert.ok(orientation.angleTo(camera.quaternion)<1e-6);
  h.topology.dispose();
});

test("box zoom frames a reversed drag, and Escape cancels an unfinished box", () => {
  const h=topologyHarness();
  h.topology.setArtifacts([{key:"root",type:"custom"}]);h.topology.activate();h.flush();
  const {camera}=h.renders.at(-1),{stage,box}=h.elements;
  const start=camera.position.clone(),projection=camera.projectionMatrix.clone();
  // The box spans x=400..600, y=120..280 on an 800x400 canvas.
  // Its height limits the 2.5x crop, with room on both sides.
  const samples=[[0,.4,.94],[.25,0,.98],[.5,-.4,.995]].map(([x,y,z])=>({
    world:camera.position.clone().set(x,y,z).unproject(camera),
    expected:[(x-.25)*2.5,y*2.5]
  }));
  function boxDrag(){
    stage.emit("keydown",{key:"b",ctrlKey:true});
    stage.emit("pointerdown",{button:0,pointerId:2,clientX:600,clientY:280});
    stage.emit("pointermove",{pointerId:2,clientX:400,clientY:120});
    assert.equal(box.hidden,false);assert.equal(box.style.width,"200px");
  }
  boxDrag();stage.emit("keydown",{key:"Escape"});h.flush();
  assert.equal(box.hidden,true);assert.equal(stage.dataset.mode,"orbit");
  assert.ok(camera.position.distanceTo(start)<1e-8);
  boxDrag();stage.emit("pointerup",{pointerId:2,clientX:400,clientY:120});h.flush();
  assert.equal(box.hidden,true);assert.equal(stage.dataset.mode,"orbit");
  for(const {world,expected} of samples){
    const point=world.clone().project(camera);
    assert.ok(Math.abs(point.x-expected[0])<1e-8 && Math.abs(point.y-expected[1])<1e-8,
      "box zoom must preserve the crop at every scene depth");
  }
  stage.emit("keydown",{key:"Home"});h.flush();
  assert.ok(camera.position.distanceTo(start)<1e-8 && camera.projectionMatrix.equals(projection));
  assert.deepEqual(h.selected,[]);
  h.topology.dispose();
});

test("overlapping glass is captured behind the front icon and materials are restored", () => {
  const h=topologyHarness(), {LayeredGlass,THREE:T}=h.context.WizardTopologyThree;
  const scene=new T.Scene(),camera=new T.PerspectiveCamera(45,2,.1,100);
  camera.position.z=8;camera.updateMatrixWorld();
  const glass=new T.MeshPhysicalMaterial({transmission:1});
  const nodes=[-2,0,2].map(z=>{
    const group=new T.Group(),visual=new T.Mesh(new T.BoxGeometry(1,1,1),glass);
    group.position.z=z;group.add(visual);scene.add(group);return {group,visual};
  });
  let output=null;
  const passes=[];
  const renderer={
    extensions:{has:()=>true},getDrawingBufferSize:v=>v.set(800,400),
    getRenderTarget:()=>output,setRenderTarget:t=>{output=t;},
    render(){passes.push({output, materials:nodes.map(n=>n.visual.material)});}
  };
  const compositor=new LayeredGlass(renderer);
  compositor.render(scene,camera,nodes);
  assert.equal(passes.length,3);
  assert.equal(passes[0].materials[0],glass);
  assert.equal(passes[1].materials[1],glass);
  assert.equal(passes[2].materials[2],glass);
  assert.ok(passes[2].materials[0].isShaderMaterial && passes[2].materials[1].isShaderMaterial);
  assert.ok(passes[0].output && passes[1].output && passes[0].output!==passes[1].output);
  assert.equal(passes[2].output,null);
  assert.ok(nodes.every(n=>n.visual.material===glass));
  passes.length=0;nodes[0].group.visible=false;nodes[2].group.position.x=4;
  compositor.render(scene,camera,nodes);
  assert.equal(passes.length,1,"separate silhouettes use one normal render");
  let disposed=0;
  for (const pass of compositor.targets) pass.addEventListener("dispose",()=>disposed++);
  compositor.dispose();assert.equal(disposed,2);h.topology.dispose();
});

test("directed conduits clear complete model bounds and detour around other artifacts", () => {
  const h=topologyHarness(), {THREE:T,DirectedConduits}=h.context.WizardTopologyThree;
  const edges=new DirectedConduits(), nodes=new Map();
  function add(key,x,y,z) {
    const group=new T.Group(), visual=new T.Group();
    visual.add(new T.Mesh(new T.BoxGeometry(1,1,1)));
    group.position.set(x,y,z); group.add(visual);nodes.set(key,{group,visual});
    return visual;
  }
  const source=add("source",-3,0,0), ribbon=new T.Mesh(new T.BoxGeometry(.9,.18,.18));
  ribbon.position.x=.9;source.add(ribbon);
  add("child",3,0,0);add("blocker",0,0,0);
  edges.update(nodes,[["source","child"]]);
  assert.equal(edges.group.children.length,1);
  const connection=edges.group.children[0], [shaft,head]=connection.children;
  assert.equal(connection.userData.source,"source");assert.equal(connection.userData.destination,"child");
  assert.equal(shaft.material,head.material);assert.ok(head.material.transmission>.85);
  const path=shaft.geometry.parameters.path;
  assert.ok(path.points[0].x> -3+1.35+.12,"leave clearance beyond the protruding ribbon");
  assert.ok(path.points.some(p=>Math.max(Math.abs(p.y),Math.abs(p.z))>.65),"detour around the blocker");
  const heading=new T.Vector3(0,1,0).applyQuaternion(head.quaternion);
  assert.ok(path.getTangentAt(1).dot(heading)>.99999);
  assert.ok(heading.x>.999 && head.position.x<2.5-.12);
  edges.group.updateMatrixWorld(true);
  const boxes=Array.from(nodes.values(),n=>new T.Box3().setFromObject(n.visual).expandByScalar(.12));
  for(const mesh of [shaft,head]) {
    const geometry=mesh.geometry, positions=geometry.attributes.position;
    const count=geometry.index?.count??positions.count;
    function vertex(i){return new T.Vector3().fromBufferAttribute(positions,geometry.index?.getX(i)??i).applyMatrix4(mesh.matrixWorld);}
    for(let i=0;i<count;i+=3){
      const triangle={a:vertex(i),b:vertex(i+1),c:vertex(i+2)};
      assert.ok(boxes.every(box=>!box.intersectsTriangle(triangle)),"full tube and arrow triangles clear every padded box");
    }
  }
  let disposed=0; shaft.geometry.addEventListener("dispose",()=>disposed++);
  edges.update(nodes,[["child","source"]]);
  assert.equal(disposed,1);
  const reversed=edges.group.children[0].children[1];
  assert.ok(new T.Vector3(0,1,0).applyQuaternion(reversed.quaternion).x<-.999);
  edges.dispose();
});

test("connections follow loaded model bounds, inventory filtering and context restoration", async () => {
  const h=topologyHarness();
  const inventory=[{key:"source",name:"root",type:"custom"},{key:"child",parent:"source",type:"aws:s3/stateBucket"}];
  h.topology.setArtifacts(inventory); h.topology.activate();h.flush();
  const group=()=>h.renders.at(-1).scene.getObjectByName("Directed glass conduits");
  const initial=group().children[0].children[1].position.clone();
  const thin=h.model();thin.scene.scale.y=.15;
  h.loads.get("/models/aws/s3.glb").resolve(thin);await settleModels();h.flush();
  assert.equal(group().children.length,1);
  const head=group().children[0].children[1];
  assert.ok(initial.distanceTo(head.position)>.1);
  h.topology.setArtifacts(inventory);h.flush();
  assert.equal(group().children[0].children[1],head,"identical inventory reuses geometry");
  h.elements.search.value="root";h.elements.search.emit("input");h.flush();assert.equal(group().children.length,0);
  h.elements.search.value="";h.elements.search.emit("input");h.flush();assert.equal(group().children.length,1);
  h.elements.canvas.emit("webglcontextlost");h.elements.canvas.emit("webglcontextrestored");await settleModels();h.flush();
  assert.equal(group().children.length,1);
  h.topology.dispose();
});

test("selection masks the complete visible model and restores scene and renderer state", () => {
  const h=topologyHarness();
  const {SelectionOutline, THREE:T}=h.context.WizardTopologyThree;
  const scene=new T.Scene(), camera=new T.PerspectiveCamera(), visual=h.model().scene;
  const blocker=h.model().scene.children[0], glass=visual.children[0];
  const background=new T.Color(0x374755), clearColor=new T.Color(0x303234);
  scene.add(visual,blocker);scene.background=background;
  const originals=[glass.material,blocker.material];
  const draws=[],initialTarget={};
  let target=initialTarget,alpha=.3;
  const renderer={
    autoClear:true,capabilities:{maxSamples:4},
    getClearAlpha:()=>alpha,getClearColor:c=>c.copy(clearColor),
    setClearColor:(c,a)=>{clearColor.set(c);alpha=a;},clear(){},
    getRenderTarget:()=>target,setRenderTarget:t=>{target=t;},
    getDrawingBufferSize:v=>v.set(1600,800),
    render:(s,c)=>draws.push({target,scene:s,glass:glass.layers.test(c.layers),
      blocker:blocker.layers.test(c.layers),material:glass.material})
  };
  const outline=new SelectionOutline(renderer);
  outline.render(scene,camera,visual,800,400);
  assert.deepEqual(draws.slice(0,2).map(d=>[d.glass,d.blocker]),[[true,false],[false,true]]);
  assert.equal(draws[0].material,outline.silhouette);
  assert.equal(draws[0].target,draws[1].target);
  assert.equal(draws[2].target,initialTarget);
  assert.equal(draws[2].scene,outline.overlay);
  assert.deepEqual([glass.material,blocker.material],originals);
  assert.equal(camera.layers.mask,1);assert.equal(glass.layers.mask,1);
  assert.equal(scene.background,background);assert.equal(renderer.autoClear,true);
  assert.equal(clearColor.getHex(),0x303234);assert.equal(alpha,.3);
  outline.dispose();h.topology.dispose();
});

test("nested valve glass resolves the passage before its shells from opposite views", () => {
  const h=topologyHarness(), {LayeredGlass,THREE:T}=h.context.WizardTopologyThree;
  const scene=new T.Scene(), camera=new T.PerspectiveCamera(40,1,.1,100);
  const group=new T.Group(), visual=new T.Group();
  group.add(visual);scene.add(group);
  const parts=[1,2,3].map((size,layer)=>{
    const part=new T.Mesh(new T.BoxGeometry(size,size,size),new T.MeshPhysicalMaterial({transmission:1}));
    part.userData.transmissionLayer=layer;visual.add(part);return part;
  });
  const materials=parts.map(p=>p.material),passes=[];
  let target=null;
  const compositor=new LayeredGlass({
    extensions:{has:()=>true}, getDrawingBufferSize:v=>v.set(64,64),
    getRenderTarget:()=>target,setRenderTarget:t=>{target=t;},
    render(){passes.push(parts.findIndex((p,i)=>p.material===materials[i]));}
  });
  for (const z of [-8,8]) {
    camera.position.z=z;camera.lookAt(0,0,0);camera.updateMatrixWorld();
    compositor.render(scene,camera,[{group,visual}]);
    assert.deepEqual(passes.splice(0),[0,1,2]);
    assert.deepEqual(parts.map(p=>p.material),materials);
  }
  compositor.dispose();h.topology.dispose();
});

test("separate towers share a ground plane and support their roots on hexagons", () => {
  const inventory=[
    {key:"short",component:true},{key:"key",parent:"short"},{key:"table",parent:"short"},
    {key:"tall",component:true},{key:"function",parent:"tall"},
    {key:"permit",parent:"tall",attachment:"function"}
  ];
  const nodes=new Map(layoutArtifacts(inventory).map(a=>[a.key,a]));
  assert.ok(nodes.get("tall").position[1]>nodes.get("short").position[1]);
  assert.equal(nodes.get("key").position[1],0);
  assert.equal(nodes.get("permit").position[1],0);
  const left=nodes.get("key").shelf,right=nodes.get("permit").shelf;
  assert.ok(Math.abs(left.x-right.x)>(left.width+right.width)/2+4);
  const h=topologyHarness();
  h.topology.setArtifacts(inventory);h.topology.activate();h.flush();
  assert.ok(h.loads.has("/models/pulumi/component.glb"));
  const scene=h.renders.at(-1).scene;
  const floors=scene.children.filter(o=>o.name.endsWith(" floor"));
  assert.equal(floors.length,5);
  const hex=floors.filter(o=>o.name==="Hexagonal root floor");
  assert.equal(hex.length,2);
  assert.ok(hex.every(o=>o.geometry.attributes.position.count===6*4 && o.material.opacity<.2));
  assert.equal(scene.getObjectByName("AWS attachment conduits").children.length,1);
  assert.equal(scene.getObjectByName("Directed glass conduits").children.length,3);
  h.elements.search.value="permit";h.elements.search.emit("input");h.flush();
  assert.equal(floors.filter(o=>o.visible).length,1);
  h.topology.dispose();
});

test("each parent centers over its children's split hexagons even with uneven branch sizes", () => {
  const inventory=[{key:"group",component:true},...["lambda","role","api","table","bucket"].map(key=>({key,parent:"group"}))];
  for (const [parent,count] of [["lambda",4],["role",1],["api",2]]) {
    for(let i=0;i<count;i++) inventory.push({key:parent+i,parent:"group",attachment:parent});
  }
  const nodes=layoutArtifacts(inventory),byKey=new Map(nodes.map(a=>[a.key,a]));
  for (const parent of nodes.filter(a=>a.children.length)) {
    const floors=[...new Set(parent.children.map(a=>a.shelf))];
    for(const [axis,size,index] of [["x","width",0],["z","length",2]]) {
      const min=Math.min(...floors.map(f=>f[axis]-f[size]/2));
      const max=Math.max(...floors.map(f=>f[axis]+f[size]/2));
      assert.ok(Math.abs((min+max)/2-parent.position[index])<1e-9,parent.key);
    }
    assert.ok(parent.children.every(c=>c.position[1]<parent.position[1]));
  }
  const central=byKey.get("table").shelf;
  assert.equal(central,byKey.get("bucket").shelf);
  for(const key of ["lambda","role","api"]) assert.notEqual(byKey.get(key).shelf,central);
  const floors=[...new Set(nodes.map(a=>a.shelf))];
  for(const [i,f] of floors.entries()) for(const g of floors.slice(i+1).filter(g=>g.y===f.y)) {
    assert.ok(Math.hypot(f.x-g.x,f.z-g.z)>(f.width+g.width)/2);
  }
  const reversed=layoutArtifacts(inventory.slice().reverse());
  for(const item of reversed) assert.deepEqual(item.position,byKey.get(item.key).position);
});

test("Arrows toggles both conduit groups and preserves camera, selection and floors", () => {
  const h=topologyHarness();
  h.topology.setArtifacts([{key:"root",component:true},{key:"fn",parent:"root"},
    {key:"permission",parent:"root",attachment:"fn"}]);
  h.topology.activate();h.topology.setSelection("permission");h.flush();
  const {scene,camera}=h.renders.at(-1),eye=camera.position.clone();
  const groups=["Directed glass conduits","AWS attachment conduits"].map(name=>scene.getObjectByName(name));
  const floors=scene.children.filter(o=>o.name.endsWith(" floor"));
  for(const shown of [false,true]) {
    h.elements.arrows.checked=shown;h.elements.arrows.emit("change");h.flush();
    assert.ok(groups.every(g=>g.visible===shown && g.children.length===1));
    assert.ok(floors.every(f=>f.visible && f.geometry.attributes.position.count===24));
    assert.ok(h.elements.selectedLabel.textContent.includes("permission"));
    assert.ok(camera.position.equals(eye));
  }
  h.topology.dispose();
});
