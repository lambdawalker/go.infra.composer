"use strict";

// AWS models are bundled locally. Labels and controls remain DOM elements
// so the inventory stays usable without WebGL and with a keyboard.
(() => {
  const categories = {
    component: {label:"Component group", shape:"sphere", color:[.42,.72,.83]},
    compute: {label:"Compute", shape:"pyramid", color:[.45,.67,1]},
    storage: {label:"Storage", shape:"cube", color:[.38,.77,.70]},
    network: {label:"API & DNS", shape:"sphere", color:[.68,.59,.94]},
    security: {label:"Identity & security", shape:"diamond", color:[.88,.72,.42]},
    message: {label:"Email & events", shape:"cone", color:[.93,.60,.42]},
    other: {label:"Other", shape:"cube", color:[.68,.72,.79]}
  };

  function categoryFor(type) {
    if (/lambda\/function|:Function$/i.test(type)) return "compute";
    if (/dynamodb|^aws:(?:rds|neptune)[/:]|s3\/|storage|:Bucket$|:Table$/i.test(type)) return "storage";
    if (/iam|cognito|kms|secretsmanager|:Authorizer$|lambda\/permission|:Permission$|random\/|:RandomBytes/i.test(type)) return "security";
    if (/apigateway|route53|cloudflare|acm\/|:Certificate|:DnsRecord|:DomainName/i.test(type)) return "network";
    if (/ses|cloudwatch\/event|:EmailIdentity|:EventRule|:EventTarget/i.test(type)) return "message";
    return "other";
  }

  // Both views consume this forest. Keep the original Pulumi parent available
  // for details even when an AWS attachment introduces another display level.
  function hierarchy(artifacts) {
    const byKey = new Map(artifacts.map(a => [a.key,a]));
    const parents = new Map();
    for (const a of artifacts) {
      const key = byKey.has(a.attachment) ? a.attachment : a.parent;
      if (key && key !== a.key && byKey.has(key)) parents.set(a.key,key);
    }
    const done = new Set();
    for (const a of artifacts) {
      const path = new Set();
      let key = a.key;
      while (key && !done.has(key)) {
        if (path.has(key)) { parents.delete(key); break; }
        path.add(key);
        key = parents.get(key);
      }
      for (const visited of path) done.add(visited);
    }
    return parents;
  }

  const tileRadius = 1.8;
  const hexApothem = Math.sqrt(3)/2;
  const branchGap = .8;

  // Siblings without descendants share one central tile. Pack large families
  // in concentric rings so a long list of permissions stays compact.
  function leafTile(members) {
    const offsets = [];
    const spacing = 2.7;
    if (members.length === 1 || members.length > 6) offsets.push([0,0]);
    let extent = 0;
    for (let ring=1; offsets.length<members.length; ring++) {
      const count = Math.min(6*ring,members.length-offsets.length);
      extent = members.length<=6 ? spacing/(2*Math.sin(Math.PI/count)) : spacing*ring;
      for (let i=0;i<count;i++) {
        const angle = Math.PI + i*2*Math.PI/count;
        offsets.push([extent*Math.cos(angle),extent*Math.sin(angle)]);
      }
    }
    // The apothem must enclose the outermost model and its edge clearance.
    const radius = Math.max(tileRadius,(extent+1.3)/hexApothem);
    return {members,offsets,radius,x:0,z:0};
  }

  // Measure descendants first. Each branch has an angular sector wide enough
  // for its entire subtree, so a wider landing can move its parent outwards.
  function planBranch(item) {
    const children = item.children.slice().sort((a,b)=>a.key.localeCompare(b.key));
    const leaves = children.filter(child=>!child.children.length);
    const branches = children.filter(child=>child.children.length);
    const center = leaves.length ? leafTile(leaves) : null;
    const tiles = center ? [center] : [];
    let height = leaves.length ? 1 : 0;
    for (const [index,child] of branches.entries()) {
      const plan = planBranch(child);
      const angle = Math.PI + 2*Math.PI*index/branches.length;
      const inner = center ? center.radius+branchGap+plan.radius : 0;
      const outer = branches.length>1 ? (plan.radius+branchGap/2)/Math.sin(Math.PI/branches.length) : 0;
      const distance = Math.max(inner,outer);
      tiles.push({members:[child],offsets:[[0,0]],radius:tileRadius,
        x:distance*Math.cos(angle),z:distance*Math.sin(angle),plan});
      height = Math.max(height,1+plan.height);
    }
    let radius = tileRadius;
    if (tiles.length) {
      // Center the parent over the actual outline of its immediate children's
      // floor, including split tiles of different sizes, in both ground axes.
      const cx=(Math.min(...tiles.map(t=>t.x-t.radius))+Math.max(...tiles.map(t=>t.x+t.radius)))/2;
      const cz=(Math.min(...tiles.map(t=>t.z-t.radius*hexApothem))+Math.max(...tiles.map(t=>t.z+t.radius*hexApothem)))/2;
      for (const t of tiles) {
        t.x-=cx; t.z-=cz;
        radius=Math.max(radius,Math.hypot(t.x,t.z)+(t.plan?.radius ?? t.radius));
      }
    }
    return {tiles,radius,height};
  }

  function placeTile(tile,x,storey,z,root=false) {
    const y=storey*3.6;
    const shelf={x,y,z,width:2*tile.radius,length:2*tile.radius*hexApothem,root};
    tile.members.forEach((item,i)=>{
      const [dx,dz]=tile.offsets[i];
      item.shelf=shelf;
      item.position=[x+dx,y,z+dz];
    });
    for(const child of tile.plan?.tiles ?? []) placeTile(child,x+child.x,storey-1,z+child.z);
  }

  // Size each tower from its widest shelf before reserving aisles. The lowest
  // occupied storey is at zero even when adjacent towers have different depths.
  function layoutArtifacts(artifacts) {
    const parents = hierarchy(artifacts);
    const items = artifacts.map(a => ({
      key:a.key, name:String(a.name || a.key), type:String(a.type || "Resource"),
      iconVariant:a.iconVariant, description:String(a.description || ""), action:String(a.action || "unknown"),
      parent:parents.get(a.key) || "", attachment:!!a.attachment && parents.get(a.key)===a.attachment,
      category:a.component ? "component" : categoryFor(String(a.type || "")), children:[]
    }));
    const byKey = new Map(items.map(a => [a.key,a]));
    for (const a of items) byKey.get(a.parent)?.children.push(a);
    const byName = (a,b) => a.key.localeCompare(b.key);
    const towers = items.filter(a => !a.parent).sort(byName).map(root => {
      const plan=planBranch(root);
      return {root,plan,width:2*plan.radius,length:2*plan.radius};
    });
    const columns = Math.ceil(Math.sqrt(towers.length));
    let z = 0;
    for (let i=0;i<towers.length;i+=columns) {
      const row = towers.slice(i,i+columns);
      const length = Math.max(...row.map(t=>t.length));
      let x = -(.5*row.reduce((sum,t)=>sum+t.width,0) + (row.length-1)*2.6);
      for (const tower of row) {
        const cx=x+tower.width/2, cz=z+length/2;
        placeTile({members:[tower.root],offsets:[[0,0]],radius:tileRadius,plan:tower.plan},
          cx,tower.plan.height,cz,true);
        x+=tower.width+5.2;
      }
      z+=length+5.2;
    }
    // Keep the complete grid centered on the ground and its shadow camera.
    const offset=(z-5.2)/2;
    for (const shelf of new Set(items.map(item=>item.shelf))) shelf.z-=offset;
    for (const item of items) item.position[2]-=offset;
    return items;
  }

  function selectionFamily(item,byKey) {
    if(!item)return null;
    const keys=new Set();
    for(let a=item;a;a=byKey.get(a.parent))keys.add(a.key);
    const pending=[...item.children];
    while(pending.length){
      const child=pending.pop();
      keys.add(child.key);pending.push(...child.children);
    }
    return keys;
  }

  // A taut outer boundary follows the exposed hex edges and bridges empty
  // space between bottom tiles. Exclude higher storeys from the footprint.
  function bottomPerimeters(items) {
    const result=new Map();
    for(const group of items.filter(item=>item.category==="component")){
      const pending=[group];
      let bottom=Infinity,leaves=[];
      while(pending.length){
        const item=pending.pop();
        pending.push(...item.children);
        if(item.children.length)continue;
        if(item.shelf.y<bottom){bottom=item.shelf.y;leaves=[];}
        if(item.shelf.y===bottom)leaves.push(item);
      }
      const keys=leaves.map(item=>item.key).sort(),id=JSON.stringify(keys);
      if(result.has(id)){result.get(id).groups.push(group.key);continue;}
      const points=[];
      for(const shelf of new Set(leaves.map(item=>item.shelf))){
        const radius=shelf.width/2+.13;
        for(let i=0;i<6;i++){
          const angle=i*Math.PI/3;
          points.push([shelf.x+radius*Math.cos(angle),shelf.z+radius*Math.sin(angle)]);
        }
      }
      points.sort((a,b)=>a[0]-b[0]||a[1]-b[1]);
      function halfHull(sequence){
        const hull=[];
        for(const p of sequence){
          while(hull.length>=2){
            const [a,b]=hull.slice(-2);
            if((b[0]-a[0])*(p[1]-a[1])-(b[1]-a[1])*(p[0]-a[0])>1e-8)break;
            hull.pop();
          }
          hull.push(p);
        }
        hull.pop();return hull;
      }
      const ring=halfHull(points).concat(halfHull(points.reverse()));
      result.set(id,{groups:[group.key],keys,ring,y:bottom-1.025});
    }
    return [...result.values()];
  }

  // Fifty is the authored glTF finish. Below it, reduce transmission.
  // Above it, fade the glass surfaces themselves, including their reflections.
  function applyGlassTransparency(visual,value,originals) {
    visual.traverse(object=>{
      if(!object.isMesh)return;
      for(const m of Array.isArray(object.material)?object.material:[object.material]){
        let base=originals.get(m);
        if(!base){
          if(!(m.transmission>0))continue;
          base={transmission:m.transmission,transparent:m.transparent,opacity:m.opacity,
            depthWrite:m.depthWrite,visible:m.visible};
          originals.set(m,base);
        }
        if(base.value===value)continue;
        const clear=Math.max(0,(value-50)/50);
        m.transmission=base.transmission*Math.min(1,value/50);
        m.opacity=value<50 ? 1-(1-base.opacity)*value/50 : base.opacity*(1-clear);
        m.transparent=base.transparent||clear>0;
        m.depthWrite=base.depthWrite && clear===0;
        m.visible=base.visible && value<100;
        m.needsUpdate=true;
        base.value=value;
      }
    });
  }

  // Four concentric hexagons give each glass slab a narrow beveled rim.
  function shelfGeometry(T, shelf) {
    const radius=shelf.width/2;
    const perimeter=Array.from({length:6},(_,i)=>{
      const angle=Math.PI*i/3;
      return [radius*Math.cos(angle),radius*Math.sin(angle)];
    });
    const positions=[],indices=[],n=perimeter.length;
    for (const [y,inset] of [[0,.045],[.035,0],[.12,0],[.155,.045]]) {
      for (const [px,pz] of perimeter) positions.push(px*(1-inset/radius),y,pz*(1-inset/radius));
    }
    for(let ring=0;ring<3;ring++) for(let i=0;i<n;i++) {
      const a=ring*n+i,b=ring*n+(i+1)%n;
      indices.push(a,a+n,b,b,a+n,b+n);
    }
    for(let i=1;i<n-1;i++) indices.push(0,i,i+1,3*n,3*n+i+1,3*n+i);
    const geometry=new T.BufferGeometry();
    geometry.setAttribute("position",new T.Float32BufferAttribute(positions,3));
    geometry.setIndex(indices);
    geometry.computeVertexNormals();
    return geometry;
  }
  const dot = (a,b) => a[0]*b[0]+a[1]*b[1]+a[2]*b[2];
  const cross = (a,b) => [a[1]*b[2]-a[2]*b[1],a[2]*b[0]-a[0]*b[2],a[0]*b[1]-a[1]*b[0]];
  const normal = a => {const length=Math.hypot(...a)||1;return a.map(v=>v/length);};
  const subtract = (a,b) => a.map((v,i)=>v-b[i]);
  function perspective(aspect) {
    const f=1/Math.tan(Math.PI/8),near=.1,far=1000;
    return new Float32Array([f/aspect,0,0,0, 0,f,0,0, 0,0,(far+near)/(near-far),-1,
      0,0,2*far*near/(near-far),0]);
  }
  function lookAt(eye,target) {
    const z=normal(subtract(eye,target)),x=normal(cross([0,1,0],z)),y=cross(z,x);
    return new Float32Array([x[0],y[0],z[0],0, x[1],y[1],z[1],0, x[2],y[2],z[2],0,
      -dot(x,eye),-dot(y,eye),-dot(z,eye),1]);
  }
  function multiply(a,b) {
    const result=new Float32Array(16);
    for(let column=0;column<4;column++)for(let row=0;row<4;row++)for(let k=0;k<4;k++)
      result[column*4+row]+=a[k*4+row]*b[column*4+k];
    return result;
  }
  function project(point,matrix,width,height) {
    const [x,y,z]=point,w=matrix[3]*x+matrix[7]*y+matrix[11]*z+matrix[15];
    if(w<=0)return null;
    const px=(matrix[0]*x+matrix[4]*y+matrix[8]*z+matrix[12])/w;
    const py=(matrix[1]*x+matrix[5]*y+matrix[9]*z+matrix[13])/w;
    return {x:(px+1)*width/2,y:(1-py)*height/2,visible:Math.abs(px)<=1.05&&Math.abs(py)<=1.05};
  }

  function modelPathFor(type, iconFor, variant) {
    const icon = typeof iconFor === "function" ? iconFor(type, variant) : "";
    const match = /^\/icons\/((?:aws|cloudflare|random)\/[a-z0-9-]+)\.svg$/.exec(icon || "");
    return match ? "/models/" + match[1] + ".glb" : "";
  }

  function matchesArtifact(item, query) {
    return !query || item.name.toLowerCase().includes(query) || item.type.toLowerCase().includes(query);
  }

  function visibleEdges(items, query) {
    const visible = new Set(items.filter(item => matchesArtifact(item, query)).map(item => item.key));
    return items.filter(item => visible.has(item.key) && visible.has(item.parent)).map(item => [item.parent,item.key]);
  }

  // An opaque surface gives the glTF transmission pass something to sample.
  // The grid fades before the horizon and when it becomes smaller than a pixel.
  function createFloor(T) {
    const material=new T.ShaderMaterial({
      toneMapped:false,lights:true,
      uniforms:T.UniformsUtils.merge([T.UniformsLib.lights,{
        backdrop:{value:new T.Color()}, surface:{value:new T.Color()},
        grid:{value:new T.Color()}, radius:{value:24}
      }]),
      vertexShader:`
        #include <common>
        #include <shadowmap_pars_vertex>
        varying vec2 ground;
        void main() {
          vec4 world = modelMatrix * vec4(position, 1.0);
          ground = world.xz;
          #include <beginnormal_vertex>
          #include <defaultnormal_vertex>
          #include <begin_vertex>
          #include <project_vertex>
          #include <worldpos_vertex>
          #include <shadowmap_vertex>
        }`,
      fragmentShader:`
        #include <common>
        #include <bsdfs>
        #include <lights_pars_begin>
        #include <shadowmap_pars_fragment>
        #include <shadowmask_pars_fragment>
        uniform vec3 backdrop, surface, grid;
        uniform float radius;
        varying vec2 ground;
        void main() {
          vec2 cell = ground / 2.0;
          vec2 footprint = max(fwidth(cell), vec2(0.0001));
          vec2 line = abs(fract(cell + 0.5) - 0.5) / footprint;
          float ink = (1.0 - smoothstep(0.0, 1.1, min(line.x, line.y)))
                    * (1.0 - smoothstep(0.15, 0.6, max(footprint.x, footprint.y)));
          float falloff = 1.0 - smoothstep(0.0, radius, length(ground));
          vec3 litFloor = mix(surface, grid, ink) * mix(0.43, 1.0, getShadowMask());
          gl_FragColor = vec4(mix(backdrop, litFloor, falloff), 1.0);
          #include <colorspace_fragment>
        }`
    });
    const floor=new T.Mesh(new T.PlaneGeometry(2,2),material);
    floor.receiveShadow=true;
    floor.rotation.x=-Math.PI/2;
    floor.position.y=-1.05;
    return floor;
  }

  function createArtifactTopology({stage,canvas,tooltip,message,selectedLabel,list,search,count,fit,tlsModel,arrows,focus,platformOpacity,glassTransparency,navigation,axes,box,onSelect}) {
    let items=[],byKey=new Map(),selectedKey="",active=false,drawQueued=false,pointer=null;
    const openingYaw=-.18,openingPitch=.24,pitchLimit=Math.PI/2-.00001;
    let mode="orbit",yaw=openingYaw,pitch=openingPitch,distance=30,target=[0,0,0],contextLost=false,disposed=false;
    // Normalized size and center offset of the viewport inside the full perspective frame.
    let cropSize=1,cropX=0,cropY=0;
    let layeredGlass=null,renderer=null,scene=null,camera=null,runtime=null,edges=null,outline=null,environment=null;
    let keyLight=null,floor=null,raycaster=null,mouse=null,edgesDirty=true;
    const nodes=new Map(),modelCache=new Map(),loadingModels=new Set(),failedModels=new Set();
    const fallbackGeometry=new Map(),fallbackMaterials=new Map();
    const listeners=[];
    let attachmentEdges=null,shelfMaterial=null,rimMaterial=null,shelfSignature="";
    const shelves=new Map(),perimeters=[];
    const originalGlass=new WeakMap();
    let focusBlend=null,family=null,perimeterMaterial=null;
    function listen(element,event,handler,options) {
      element.addEventListener(event,handler,options);
      listeners.push(()=>element.removeEventListener(event,handler,options));
    }
    const resizeObserver=typeof ResizeObserver!=="undefined"?new ResizeObserver(()=>requestDraw()):null;
    resizeObserver?.observe(stage);
    listen(window,"resize",()=>requestDraw());
    listen(document,"visibilitychange",()=>{if(!document.hidden)requestDraw();});
    const systemTheme=window.matchMedia?.("(prefers-color-scheme: light)");
    if(systemTheme)listen(systemTheme,"change",()=>requestDraw());
    const themeObserver=new MutationObserver(()=>requestDraw());
    themeObserver.observe(document.documentElement,{attributes:true,attributeFilter:["data-theme"]});

    listen(arrows,"change",()=>requestDraw());
    listen(focus,"change",()=>requestDraw());
    for(const slider of [platformOpacity,glassTransparency]){
      listen(slider,"input",()=>{
        if(slider.nextElementSibling)slider.nextElementSibling.value=slider.value+"%";
        slider.setAttribute("aria-valuetext",slider.value+"%");
        requestDraw();
      });
    }
    function updateGlass(){
      for(const node of nodes.values())applyGlassTransparency(node.visual,Number(glassTransparency.value),originalGlass);
      if(renderer)renderer.shadowMap.needsUpdate=true;
      requestDraw();
    }
    listen(glassTransparency,"input",updateGlass);

    const axisTips=[];
    for(const [index,axis] of ["X","Y","Z"].entries()){
      const arm=document.createElement("span");
      arm.className="axis-arm axis-"+axis.toLowerCase();
      axes.append(arm);
      for(const sign of [1,-1]){
        const tip=document.createElement("button"),label=(sign>0?"+":"−")+axis;
        tip.type="button";tip.className="axis-tip axis-"+axis.toLowerCase()+(sign<0?" negative":"");
        tip.textContent=sign>0?axis:"";tip.title="View from "+label;
        tip.setAttribute("aria-label",tip.title);axes.append(tip);
        axisTips.push({index,sign,tip,arm});
        listen(tip,"click",()=>{
          const direction=camera.matrixWorld.elements[8+index];
          const side=sign*direction>.99999?-sign:sign;
          yaw=index===0?side*Math.PI/2:index===2&&side<0?Math.PI:0;
          pitch=index===1?side*pitchLimit:0;
          requestDraw();
        });
      }
    }
    function orientAxes(){
      const basis=camera.matrixWorld.elements;
      for(const {index,sign,tip,arm} of axisTips){
        const x=32*sign*basis[index],y=-32*sign*basis[4+index];
        tip.style.left=(46+x)+"px";tip.style.top=(46+y)+"px";
        tip.style.zIndex=String(12+Math.round(10*sign*basis[8+index]));
        if(sign===1){
          arm.style.width=Math.hypot(x,y)+"px";
          arm.style.transform="rotate("+Math.atan2(y,x)+"rad)";
        }
      }
    }
    const navButtons=Array.from(navigation.querySelectorAll("button")).filter(button=>button.dataset.nav);
    function startBoxZoom(){
      cancelGesture();setMode("box");stage.focus({preventScroll:true});
    }
    function setMode(next){
      mode=next;stage.dataset.mode=next;
      for(const button of navButtons)if(["pan","box"].includes(button.dataset.nav))
        button.setAttribute("aria-pressed",String(next===button.dataset.nav));
      tooltip.hidden=true;
    }
    function zoom(factor){
      if(factor>1 && cropSize<1){
        const next=Math.min(1,cropSize*factor);
        const recenter=(1-next)/(1-cropSize);
        cropX*=recenter;cropY*=recenter;
        factor/=next/cropSize;cropSize=next;
      }
      if(Math.abs(factor-1)>1e-10)distance=Math.max(5,Math.min(500,distance*factor));
      requestDraw();
    }
    function resetView(){
      cancelGesture();yaw=openingYaw;pitch=openingPitch;frame(items);
    }
    for(const button of navButtons)listen(button,"click",()=>{
      const action=button.dataset.nav;
      if(action==="home")resetView();
      else if(action==="in"||action==="out")zoom(action==="in"?.8:1.25);
      else {cancelGesture(false);setMode(mode===action?"orbit":action);}
      stage.focus({preventScroll:true});
    });
    function pathFor(item) {
      if (item.category === "component") return "/models/pulumi/component.glb";
      const path=modelPathFor(item.type,window.wizardAWSIconFor,item.iconVariant);
      return path==="/models/aws/acm.glb" && tlsModel.value==="shield" ? "/models/aws/acm-shield.glb" : path;
    }
    listen(tlsModel,"change",()=>{syncArtifacts();updateMessage();requestDraw();});

    function setMessage(text) {message.textContent=text;message.hidden=!text;}
    function updateMessage() {
      navigation.hidden=!items.length||!renderer||contextLost;
      if(!items.length)setMessage("Select the deployment target to view its artifacts.");
      else if(contextLost)setMessage("3D view is temporarily unavailable. Select an artifact from the list below.");
      else if(!renderer)setMessage("3D view is unavailable here. Select an artifact from the list below.");
      else if(items.some(item=>loadingModels.has(pathFor(item))))setMessage("Loading resource models…");
      else if(items.some(item=>failedModels.has(pathFor(item))))setMessage("Some resource models could not be loaded. Those artifacts use basic shapes.");
      else setMessage("");
    }
    function initialize() {
      if(renderer)return true;
      if(disposed||contextLost)return false;
      try {
        runtime=window.WizardTopologyThree;
        if(!runtime?.THREE||!runtime.GLTFLoader||!runtime.RoomEnvironment)throw Error("3D runtime unavailable");
        const T=runtime.THREE;
        renderer=new T.WebGLRenderer({canvas,antialias:true,alpha:false});
        layeredGlass=new runtime.LayeredGlass(renderer);
        renderer.shadowMap.enabled=true;
        renderer.shadowMap.autoUpdate=false;
        renderer.outputColorSpace=T.SRGBColorSpace;
        renderer.toneMapping=T.ACESFilmicToneMapping;
        renderer.toneMappingExposure=1.0;
        scene=new T.Scene();camera=new T.PerspectiveCamera(45,1,.1,1000);
        raycaster=new T.Raycaster();mouse=new T.Vector2();
        const room=new runtime.RoomEnvironment(),generator=new T.PMREMGenerator(renderer);
        try {environment=generator.fromScene(room,.015);scene.environment=environment.texture;}
        finally {room.dispose?.();generator.dispose();}
        scene.environmentIntensity=.78;
        scene.add(new T.HemisphereLight(0xe8f2ff,0x44374a,.45));
        keyLight=new T.DirectionalLight(0xffead9,1.65);
        keyLight.castShadow=true;
        keyLight.shadow.mapSize.setScalar(Math.min(2048,renderer.capabilities.maxTextureSize));
        keyLight.shadow.bias=-.00015;
        keyLight.shadow.radius=1.5;
        const rim=new T.DirectionalLight(0xc3e1ff,2.0);
        rim.position.set(4,5,-6);
        floor=createFloor(T);
        scene.add(keyLight,rim,floor);
        edges=new runtime.DirectedConduits();
        attachmentEdges=new runtime.DirectedConduits();
        attachmentEdges.group.name="AWS attachment conduits";
        attachmentEdges.material.color.setHex(0xd5fbff);
        attachmentEdges.material.attenuationColor.setHex(0x62dbeb);
        attachmentEdges.material.attenuationDistance=.055;
        edges.material.attenuationDistance=.07;
        scene.add(edges.group,attachmentEdges.group);
        shelfMaterial=new T.MeshPhysicalMaterial({color:0xb4dded,roughness:.18,metalness:.08,
          clearcoat:.8,transparent:true,opacity:.095,depthWrite:false});
        rimMaterial=new T.LineBasicMaterial({color:0xa0cee4,transparent:true,opacity:.32,depthWrite:false});
        perimeterMaterial=new T.LineBasicMaterial({color:0x9ecde5,transparent:true,depthWrite:false});
        focusBlend=new runtime.SelectionFocus(renderer);
        outline=new runtime.SelectionOutline(renderer);
        edgesDirty=true;syncArtifacts();canvas.hidden=false;updateMessage();
        return true;
      } catch {
        releaseRenderer();canvas.hidden=true;updateMessage();return false;
      }
    }
    function releaseRenderer() {
      nodes.clear();
      for(const geometry of fallbackGeometry.values())geometry.dispose();fallbackGeometry.clear();
      for(const material of fallbackMaterials.values())material.dispose();fallbackMaterials.clear();
      clearShelves();perimeterMaterial?.dispose();focusBlend?.dispose();focusBlend=null;shelfMaterial?.dispose();rimMaterial?.dispose();shelfSignature="";
      attachmentEdges?.dispose();attachmentEdges=null;edges?.dispose();outline?.dispose();
      floor?.geometry.dispose();floor?.material.dispose();floor=null;
      keyLight?.shadow.dispose();keyLight=null;
      layeredGlass?.dispose();layeredGlass=null;
      environment?.dispose();renderer?.dispose();
      renderer=null;scene=null;camera=null;edges=null;outline=null;environment=null;raycaster=null;mouse=null;
    }
    function fallbackFor(category) {
      const T=runtime.THREE;
      if(!fallbackGeometry.has(category)) {
        const geometry=category==="compute"?new T.ConeGeometry(.95,1.8,4):
          category==="component"?new T.SphereGeometry(.85,32,24):
          category==="network"?new T.SphereGeometry(.85,16,10):
          category==="security"?new T.OctahedronGeometry(1):
          category==="message"?new T.ConeGeometry(.85,1.8,16):new T.BoxGeometry(1.7,1.7,1.7);
        fallbackGeometry.set(category,geometry);
        const tint=new T.Color(...categories[category].color);
        fallbackMaterials.set(category,new T.MeshPhysicalMaterial({
          color:tint.clone().lerp(new T.Color(0xffffff),.35),attenuationColor:tint,
          roughness:.105,transmission:1,ior:1.48,thickness:.16,attenuationDistance:.19,
          clearcoat:.28,clearcoatRoughness:.1
        }));
      }
      const mesh=new T.Mesh(fallbackGeometry.get(category),fallbackMaterials.get(category));
      mesh.castShadow=true;
      return mesh;
    }
    function loadModel(path) {
      if(!modelCache.has(path)) {
        loadingModels.add(path);
        const pending=new runtime.GLTFLoader().loadAsync(path).then(gltf=>{
          const T=runtime.THREE,template=new T.Group(),centered=new T.Group();
          const bounds=new T.Box3().setFromObject(gltf.scene),size=bounds.getSize(new T.Vector3());
          const extent=Math.max(size.x,size.y,size.z);
          if(!Number.isFinite(extent)||extent<=0)throw Error("Empty model");
          centered.position.copy(bounds.getCenter(new T.Vector3())).multiplyScalar(-1);
          gltf.scene.traverse(object=>{if(object.isMesh)object.castShadow=true;});
          centered.add(gltf.scene);template.add(centered);template.scale.setScalar(1.7/extent);
          template.position.y=-.895+size.y*1.7/(2*extent);
          if(disposed){disposeTemplate(template);return null;}
          return template;
        }).catch(()=>{if(!disposed)failedModels.add(path);return null;}).finally(()=>{
          loadingModels.delete(path);
          if(!disposed){updateMessage();requestDraw();}
        });
        modelCache.set(path,pending);
      }
      return modelCache.get(path);
    }
    function clearShelves() {
      for (const {mesh,outline} of shelves.values()) {
        scene.remove(mesh);mesh.geometry.dispose();outline.geometry.dispose();
      }
      shelves.clear();
      for(const {line} of perimeters){scene.remove(line);line.geometry.dispose();}
      perimeters.length=0;
    }
    function syncShelves() {
      const levels=new Map();
      for(const item of items){
        if(!levels.has(item.shelf)) levels.set(item.shelf,[]);
        levels.get(item.shelf).push(item.key);
      }
      const signature=JSON.stringify([[...levels],items.map(a=>[a.key,a.parent,a.category])]);
      if(signature===shelfSignature)return;
      shelfSignature=signature;
      clearShelves();
      const T=runtime.THREE;
      for(const boundary of bottomPerimeters(items)){
        const points=[];
        boundary.ring.forEach(([x,z],i)=>{
          const next=boundary.ring[(i+1)%boundary.ring.length];
          points.push(x,boundary.y,z,next[0],boundary.y,next[1]);
        });
        const geometry=new T.BufferGeometry();
        geometry.setAttribute("position",new T.Float32BufferAttribute(points,3));
        const line=new T.LineSegments(geometry,perimeterMaterial);
        line.name="Group base perimeter";
        scene.add(line);perimeters.push({...boundary,line});
      }
      for(const [level,keys] of levels){
        const geometry=shelfGeometry(T,level),mesh=new T.Mesh(geometry,shelfMaterial);
        const outline=new T.LineSegments(new T.EdgesGeometry(geometry,20),rimMaterial);
        mesh.name=level.root ? "Hexagonal root floor" : "Resource floor";
        mesh.position.set(level.x,level.y-1.05,level.z);
        mesh.add(outline);scene.add(mesh);shelves.set(level,{mesh,outline,keys});
      }
    }
    function syncArtifacts() {
      if(!scene)return;
      const T=runtime.THREE,keys=new Set(items.map(item=>item.key));
      for(const [key,node] of nodes)if(!keys.has(key)){scene.remove(node.group);nodes.delete(key);edgesDirty=true;}
      for(const item of items) {
        const path=pathFor(item);
        let node=nodes.get(item.key);
        if(node&&(node.path!==path||node.category!==item.category)){
          scene.remove(node.group);nodes.delete(item.key);node=null;
        }
        if(!node) {
          const group=new T.Group();group.userData.artifactKey=item.key;
          const visual=fallbackFor(item.category);applyGlassTransparency(visual,Number(glassTransparency.value),originalGlass);group.add(visual);scene.add(group);
          edgesDirty=true;node={group,visual,path,category:item.category};nodes.set(item.key,node);
          if(path) {
            const current=node;
            loadModel(path).then(template=>{
              // A refreshed inventory or restored WebGL context may have replaced this node.
              if(!template||disposed||nodes.get(item.key)!==current)return;
              current.group.remove(current.visual);
              current.visual=template.clone(true);applyGlassTransparency(current.visual,Number(glassTransparency.value),originalGlass);current.group.add(current.visual); edgesDirty=true;
              renderer.shadowMap.needsUpdate=true;requestDraw();
            });
          }
        }
        const p=node.group.position;
        if (node.attachment!==item.attachment || node.parent!==item.parent || p.x!==item.position[0] || p.y!==item.position[1] || p.z!==item.position[2]) {
          p.set(...item.position);node.parent=item.parent;node.attachment=item.attachment;edgesDirty=true;
        }
      }
      syncShelves();
      const extent=items.reduce((r,item)=>Math.max(r,Math.hypot(...item.position)),0)*1.5+3;
      const radius=extent*1.1+18;
      floor.scale.set(radius,radius,1);
      floor.material.uniforms.radius.value=radius;
      // Include the floor projection of elevated nodes in the light camera.
      keyLight.position.set(-4,7,5).normalize().multiplyScalar(extent*2);
      const shadowCamera=keyLight.shadow.camera;
      Object.assign(shadowCamera,{left:-extent,right:extent,top:extent,bottom:-extent,far:extent*4});
      shadowCamera.updateProjectionMatrix();
      renderer.shadowMap.needsUpdate=true;
      updateVisibility();
    }
    function updateVisibility() {
      if(!scene)return;
      const query=search.value.trim().toLowerCase();
      for(const {line,keys,groups} of perimeters)line.visible=[...keys,...groups].some(key=>matchesArtifact(byKey.get(key),query));
      for (const {mesh,keys} of shelves.values()) mesh.visible=keys.some(key=>matchesArtifact(byKey.get(key),query));
      for(const item of items){
        const node=nodes.get(item.key),visible=matchesArtifact(item,query);
        if(node && node.group.visible!==visible){node.group.visible=visible;edgesDirty=true;renderer.shadowMap.needsUpdate=true;}
      }
    }
    function fitView() {
      frame(items.filter(item=>matchesArtifact(item,search.value.trim().toLowerCase())));
    }
    function frame(visible){
      cropSize=1;cropX=0;cropY=0;
      if(!visible.length){target=[0,0,0];distance=30;requestDraw();return;}
      const extents=[0,1,2].map(i=>[Math.min(...visible.map(a=>a.position[i])),Math.max(...visible.map(a=>a.position[i]))]);
      target=extents.map(([min,max])=>(min+max)/2);
      const radius=Math.max(...visible.map(a=>Math.hypot(...subtract(a.position,target))))+2;
      distance=Math.max(12,radius*2.7);requestDraw();
    }
    function draw() {
      drawQueued=false;
      if(disposed||!active||document.hidden||!items.length||contextLost||!initialize())return;
      const bounds=canvas.getBoundingClientRect();if(bounds.width<2||bounds.height<2)return;
      // Supersample fine edges on standard displays, within GPU and pixel budgets.
      const ratio=Math.min(Math.max(2,window.devicePixelRatio||1),
        Math.sqrt(4_194_304/(bounds.width*bounds.height)),
        renderer.capabilities.maxTextureSize/Math.max(bounds.width,bounds.height));
      if(canvas.width!==Math.floor(bounds.width*ratio)||canvas.height!==Math.floor(bounds.height*ratio)){
        renderer.setDrawingBufferSize(bounds.width,bounds.height,ratio);
      }
      const color=getComputedStyle(stage).backgroundColor;
      scene.background=new runtime.THREE.Color(color);
      const theme=document.documentElement.dataset.theme;
      const lightTheme=theme?theme==="light":systemTheme?.matches;
      const uniforms=floor.material.uniforms;
      uniforms.backdrop.value.copy(scene.background);
      uniforms.surface.value.setHex(lightTheme?0xc2cfda:0x415061);
      uniforms.grid.value.setHex(lightTheme?0xa6b5c3:0x516174);
      const opacity=Number(platformOpacity.value)/100;
      shelfMaterial.opacity=opacity;
      rimMaterial.opacity=Math.min(1,opacity*.32/.095);
      perimeterMaterial.opacity=Math.min(1,opacity*8);
      edges.group.visible=attachmentEdges.group.visible=arrows.checked;
      if (arrows.checked && edgesDirty) {
        const links=visibleEdges(items,search.value.trim().toLowerCase());
        edges.update(nodes,links.filter(([,key])=>!byKey.get(key).attachment));
        attachmentEdges.update(nodes,links.filter(([,key])=>byKey.get(key).attachment));
        edgesDirty=false;
      }
      camera.aspect=bounds.width/bounds.height;
      if(cropSize!==1||cropX!==0||cropY!==0){
        camera.setViewOffset(bounds.width,bounds.height,
          (.5+cropX-cropSize/2)*bounds.width,(.5+cropY-cropSize/2)*bounds.height,
          cropSize*bounds.width,cropSize*bounds.height);
      }else camera.clearViewOffset();
      camera.position.set(target[0]+distance*Math.sin(yaw)*Math.cos(pitch),target[1]+distance*Math.sin(pitch),
        target[2]+distance*Math.cos(yaw)*Math.cos(pitch));
      camera.lookAt(...target);
      // Keep the orbit pivot on the center ray of an off-axis crop.
      const span=2*distance*Math.tan(camera.fov*Math.PI/360);
      camera.translateX(-cropX*span*camera.aspect);camera.translateY(cropY*span);
      camera.updateMatrixWorld();
      orientAxes();
      const context=[];
      if(focus.checked && family){
        for(const [key,node] of nodes)if(node.group.visible && !family.has(key))context.push(node.group);
        for(const {mesh,keys} of shelves.values())if(mesh.visible && !keys.some(k=>family.has(k)))context.push(mesh);
        for(const {line,groups} of perimeters)if(line.visible && !groups.some(k=>family.has(k)))context.push(line);
        for(const conduits of [edges,attachmentEdges]){
          if(!conduits.group.visible)continue;
          for(const edge of conduits.group.children){
            if(!family.has(edge.userData.source)||!family.has(edge.userData.destination))context.push(edge);
          }
        }
      }
      focusBlend.render(context,()=>layeredGlass.render(scene,camera,nodes.values()));
      const selected=nodes.get(selectedKey);
      if(selected?.group.visible)outline.render(scene,camera,selected.visual,bounds.width,bounds.height);
    }
    function requestDraw() {
      if(disposed||!active||drawQueued||contextLost)return;
      drawQueued=true;requestAnimationFrame(draw);
    }
    function updateSelection() {
      const selected=byKey.get(selectedKey);family=selectionFamily(selected,byKey);selectedLabel.hidden=!selected;
      if(selected)selectedLabel.textContent=`${selected.name} · ${categories[selected.category].label}`;
      for(const button of list.querySelectorAll("button"))button.setAttribute("aria-current",String(button.dataset.key===selectedKey));
      updateVisibility();requestDraw();
    }
    function renderList() {
      const query=search.value.trim().toLowerCase();list.replaceChildren();
      const visible=items.filter(item=>matchesArtifact(item,query));
      count.textContent=items.length?`${visible.length} of ${items.length}`:"0";
      if(items.length&&!visible.length){const empty=document.createElement("li");empty.textContent="No artifacts match this search.";empty.className="topology-empty";list.append(empty);}
      for(const item of visible){const li=document.createElement("li"),button=document.createElement("button");
        button.type="button";button.dataset.key=item.key;button.title=item.name;
        button.setAttribute("aria-label",`${item.name}, ${categories[item.category].label}, ${item.action}`);
        const marker=document.createElement("i");marker.className="topology-key "+item.category;marker.setAttribute("aria-hidden","true");
        const name=document.createElement("span");name.className="topology-list-text";name.textContent=item.name;
        const kind=document.createElement("span");kind.className="topology-list-kind";kind.textContent=categories[item.category].label;
        button.append(marker,name,kind);button.onclick=()=>onSelect(item.key);li.append(button);list.append(li);}
      updateSelection();
    }
    function setArtifacts(artifacts) {
      const previousStructure=items.map(a=>a.key+"\0"+a.parent).join("\n");
      items=layoutArtifacts(artifacts);byKey=new Map(items.map(a=>[a.key,a]));
      tlsModel.hidden=!items.some(item=>modelPathFor(item.type,window.wizardAWSIconFor,item.iconVariant)==="/models/aws/acm.glb");
      if(!byKey.has(selectedKey))selectedKey="";
      canvas.hidden=!items.length||!renderer;syncArtifacts();renderList();
      if(previousStructure!==items.map(a=>a.key+"\0"+a.parent).join("\n"))fitView();else requestDraw();
      if(!items.length||renderer)updateMessage();
    }
    function setSelection(key) {selectedKey=key||"";updateSelection();}
    function activate() {if(disposed)return;active=true;if(items.length)initialize();requestDraw();}
    function deactivate() {active=false;cancelGesture();tooltip.hidden=true;}
    function near(clientX,clientY) {
      if(!active||!renderer||!camera||contextLost)return null;
      const rect=stage.getBoundingClientRect();if(!rect.width||!rect.height)return null;
      mouse.set((clientX-rect.left)/rect.width*2-1,-(clientY-rect.top)/rect.height*2+1);
      raycaster.setFromCamera(mouse,camera);scene.updateMatrixWorld(true);
      const visuals=[...nodes.values()].filter(node=>node.group.visible).map(node=>node.visual);
      for(const hit of raycaster.intersectObjects(visuals,true)) {
        for(let object=hit.object;object;object=object.parent)if(object.userData.artifactKey)return byKey.get(object.userData.artifactKey)||null;
      }
      return null;
    }
    // Screen deltas are in CSS pixels. Use the plane through the orbit target.
    function offsetTarget(origin,dx,dy){
      const scale=cropSize*2*distance*Math.tan(camera.fov*Math.PI/360)/canvas.getBoundingClientRect().height;
      const basis=camera.matrixWorld.elements;
      return origin.map((value,i)=>value+scale*(dx*basis[i]+dy*basis[4+i]));
    }
    function cancelGesture(clearMode=true){
      const id=pointer?.id;pointer=null;
      if(id!==undefined&&stage.hasPointerCapture?.(id))stage.releasePointerCapture(id);
      stage.classList.remove("dragging");box.hidden=true;
      if(clearMode)setMode("orbit");
    }
    function boxBounds(x,y){
      const rect=canvas.getBoundingClientRect();
      const clamp=(value,max)=>Math.max(0,Math.min(max,value));
      const x0=clamp(pointer.x-rect.left,rect.width),x1=clamp(x-rect.left,rect.width);
      const y0=clamp(pointer.y-rect.top,rect.height),y1=clamp(y-rect.top,rect.height);
      return {left:Math.min(x0,x1),top:Math.min(y0,y1),width:Math.abs(x1-x0),height:Math.abs(y1-y0)};
    }
    function applyBox(bounds){
      if(bounds.width<5||bounds.height<5)return;
      const rect=canvas.getBoundingClientRect();
      const x=(bounds.left+bounds.width/2)/rect.width-.5;
      const y=(bounds.top+bounds.height/2)/rect.height-.5;
      // Limit by the longer relative side so the entire rectangle stays in frame.
      const nextSize=Math.max(.0001,cropSize*Math.max(bounds.width/rect.width,bounds.height/rect.height));
      // Use the closest projected artifact center inside the box as the orbit depth.
      let closest=Infinity;
      for(const {group} of nodes.values()){
        if(!group.visible)continue;
        const p=group.position.clone().project(camera);
        if(p.z< -1||p.z>1)continue;
        const dx=((p.x/2)-x)*rect.width,dy=((-p.y/2)-y)*rect.height;
        if(Math.abs(dx)>bounds.width/2||Math.abs(dy)>bounds.height/2)continue;
        const separation=dx*dx+dy*dy;
        if(separation<closest){
          closest=separation;distance=-camera.worldToLocal(group.position.clone()).z;
        }
      }
      cropX+=cropSize*x;cropY+=cropSize*y;cropSize=nextSize;
      // Move the pivot along the chosen ray while leaving the eye fixed. A dolly
      // would magnify foreground models more than the rectangle requests.
      const span=2*distance*Math.tan(camera.fov*Math.PI/360);
      const pivot=new runtime.THREE.Vector3(cropX*span*camera.aspect,-cropY*span,-distance);
      target=camera.localToWorld(pivot).toArray();
      requestDraw();
    }
    listen(stage,"pointerdown",event=>{
      if(!active||navigation.hidden||pointer||navigation.contains(event.target)||![0,1].includes(event.button))return;
      event.preventDefault();stage.focus({preventScroll:true});
      pointer={id:event.pointerId,x:event.clientX,y:event.clientY,yaw,pitch,target,mode:event.button===1?"pan":mode,dragged:false};
      stage.setPointerCapture?.(event.pointerId);tooltip.hidden=true;
    });
    listen(stage,"pointermove",event=>{
      if(pointer?.id===event.pointerId){
        const dx=event.clientX-pointer.x,dy=event.clientY-pointer.y;
        if(Math.hypot(dx,dy)>4)pointer.dragged=true;
        if(!pointer.dragged)return;
        if(pointer.mode==="box"){
          const bounds=boxBounds(event.clientX,event.clientY);
          for(const [key,value] of Object.entries(bounds))box.style[key]=value+"px";
          box.hidden=false;return;
        }
        stage.classList.add("dragging");
        if(pointer.mode==="pan")target=offsetTarget(pointer.target,-dx,dy);
        else {yaw=pointer.yaw+dx*.008;pitch=Math.max(-pitchLimit,Math.min(pitchLimit,pointer.pitch+dy*.008));}
        requestDraw();return;
      }
      if(navigation.contains(event.target)||mode!=="orbit"){tooltip.hidden=true;return;}
      const item=near(event.clientX,event.clientY);tooltip.hidden=!item;
      if(item){tooltip.replaceChildren();const title=document.createElement("strong"),kind=document.createElement("small");
        title.textContent=item.name;kind.textContent=categories[item.category].label;tooltip.append(title,kind);
        const rect=stage.getBoundingClientRect();tooltip.style.left=Math.max(8,Math.min(event.clientX-rect.left+12,rect.width-270))+"px";
        tooltip.style.top=Math.max(8,Math.min(event.clientY-rect.top+12,rect.height-65))+"px";}});
    listen(stage,"pointerup",event=>{
      if(pointer?.id!==event.pointerId)return;
      const gesture=pointer;
      if(gesture.mode==="box")applyBox(boxBounds(event.clientX,event.clientY));
      cancelGesture(gesture.mode==="box");
      if(gesture.mode==="orbit"&&!gesture.dragged){
        const item=near(event.clientX,event.clientY);if(item)onSelect(item.key);
      }
    });
    for(const type of ["pointercancel","lostpointercapture"])
      listen(stage,type,event=>{if(pointer?.id===event.pointerId)cancelGesture();});
    listen(window,"blur",()=>cancelGesture());
    listen(stage,"pointerleave",()=>{if(!pointer)tooltip.hidden=true;});
    listen(stage,"wheel",event=>{
      if(!active||navigation.hidden)return;event.preventDefault();
      if(!pointer)zoom(Math.exp(event.deltaY*.001));
    },{passive:false});
    listen(stage,"keydown",event=>{
      if(!active||navigation.hidden)return;
      if(event.key==="Escape")cancelGesture();
      else if(event.ctrlKey&&event.key.toLowerCase()==="b")startBoxZoom();
      else if(event.key==="Home")resetView();
      else if(pointer)return;
      else if(event.key==="ArrowLeft")yaw-=.13;else if(event.key==="ArrowRight")yaw+=.13;
      else if(event.key==="ArrowUp")pitch=Math.min(pitchLimit,pitch+.13);else if(event.key==="ArrowDown")pitch=Math.max(-pitchLimit,pitch-.13);
      else if(event.key==="+"||event.key==="=")zoom(.85);else if(event.key==="-")zoom(1.15);else return;
      event.preventDefault();requestDraw();
    });
    // A pointer over the viewport can start box zoom immediately after opening the tab.
    listen(window,"keydown",event=>{
      if(!active||navigation.hidden||event.defaultPrevented||!event.ctrlKey||event.key.toLowerCase()!=="b")return;
      if(!stage.matches(":hover")||event.target.closest?.("input,textarea,select,[contenteditable=true]"))return;
      event.preventDefault();startBoxZoom();
    });
    listen(canvas,"webglcontextlost",event=>{event.preventDefault();cancelGesture();contextLost=true;canvas.hidden=true;updateMessage();});
    listen(canvas,"webglcontextrestored",()=>{contextLost=false;releaseRenderer();if(active)initialize();requestDraw();});
    listen(search,"input",()=>{renderList();updateVisibility();requestDraw();});listen(fit,"click",fitView);
    function disposeTemplate(template) {
      const geometries=new Set(),materials=new Set(),textures=new Set();
      template.traverse(object=>{if(object.geometry)geometries.add(object.geometry);
        for(const material of Array.isArray(object.material)?object.material:[object.material])if(material){materials.add(material);
          for(const value of Object.values(material))if(value?.isTexture)textures.add(value);}});
      for(const resource of [...geometries,...materials,...textures])resource.dispose();
    }
    function dispose() {
      if(disposed)return;disposed=true;active=false;
      resizeObserver?.disconnect();themeObserver.disconnect();listeners.splice(0).forEach(remove=>remove());
      releaseRenderer();for(const pending of modelCache.values())pending.then(template=>{if(template)disposeTemplate(template);});modelCache.clear();
    }
    listen(window,"pagehide",event=>{if(!event.persisted)dispose();});
    return {setArtifacts,setSelection,activate,deactivate,fitView,dispose};
  }

  if(typeof window!=="undefined"){
    window.createArtifactTopology=createArtifactTopology;
    window.artifactHierarchy=hierarchy;
  }
  if(typeof module!=="undefined"&&module.exports)module.exports={categoryFor,layoutArtifacts,perspective,lookAt,multiply,project,modelPathFor,matchesArtifact,visibleEdges,selectionFamily,bottomPerimeters};
})();
