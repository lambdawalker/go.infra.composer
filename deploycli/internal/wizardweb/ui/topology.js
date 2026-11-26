"use strict";

// AWS models are bundled locally. Labels and controls remain DOM elements
// so the inventory stays usable without WebGL and with a keyboard.
(() => {
  const categories = {
    compute: {label:"Compute", shape:"pyramid", color:[.45,.67,1]},
    storage: {label:"Storage", shape:"cube", color:[.38,.77,.70]},
    network: {label:"API & DNS", shape:"sphere", color:[.68,.59,.94]},
    security: {label:"Identity & security", shape:"diamond", color:[.88,.72,.42]},
    message: {label:"Email & events", shape:"cone", color:[.93,.60,.42]},
    other: {label:"Other", shape:"cube", color:[.68,.72,.79]}
  };

  function categoryFor(type) {
    if (/lambda\/function|:Function$|:TestHost$|:StorageAndSigner$/i.test(type)) return "compute";
    if (/dynamodb|^aws:(?:rds|neptune)[/:]|s3\/|storage|:Bucket$|:Table$/i.test(type)) return "storage";
    if (/iam|cognito|kms|secretsmanager|:Authorizer$|lambda\/permission|:Permission$|random\/|:RandomBytes/i.test(type)) return "security";
    if (/apigateway|route53|cloudflare|acm\/|:Certificate|:DnsRecord|:DomainName/i.test(type)) return "network";
    if (/ses|cloudwatch\/event|:EmailIdentity|:EventRule|:EventTarget/i.test(type)) return "message";
    return "other";
  }

  // A stable grid of clusters keeps siblings near their parent while using a
  // vertical axis for dependency depth. Cyclic or missing parents become roots.
  function layoutArtifacts(artifacts) {
    const items = artifacts.map(a => ({
      key:String(a.key), name:String(a.name || a.key), type:String(a.type || "Resource"),
      description:String(a.description || ""), action:String(a.action || "unknown"),
      parent:String(a.parent || ""), category:categoryFor(String(a.type || "")),
      children:[], position:null
    }));
    const byKey = new Map(items.map(a => [a.key,a]));
    for (const item of items) {
      const parent = byKey.get(item.parent);
      if (parent && parent !== item) parent.children.push(item);
    }
    const placed = new Set();
    let roots = items.filter(a => !byKey.has(a.parent) || a.parent === a.key);
    // A malformed cycle may have no root. It still needs a visible node.
    if (!roots.length && items.length) roots = [items[0]];
    const columns = Math.ceil(Math.sqrt(roots.length || 1));
    let nextRoot = 0;
    function place(root) {
      const row = Math.floor(nextRoot / columns), column = nextRoot % columns;
      root.position = [(column-(columns-1)/2)*8.5,0,(row-(columns-1)/2)*8.5];
      nextRoot++;
      function childrenOf(parent, depth) {
        placed.add(parent.key);
        for (const [index,child] of parent.children.entries()) {
          if (placed.has(child.key) || child.position) continue;
          const count = parent.children.length;
          const angle = index*2*Math.PI/count - Math.PI/2 + depth*.25;
          const radius = count === 1 ? 1.3 : Math.max(1.7, Math.min(3.2, count*.42));
          child.position = [parent.position[0]+radius*Math.cos(angle), depth*2.5,
            parent.position[2]+radius*Math.sin(angle)];
          childrenOf(child,depth+1);
        }
      }
      childrenOf(root,1);
    }
    roots.forEach(root => {if (!placed.has(root.key)) place(root);});
    items.forEach(item => {if (!placed.has(item.key)) place(item);});
    return items;
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

  function modelPathFor(type, iconFor) {
    const icon = typeof iconFor === "function" ? iconFor(type) : "";
    const match = /^\/icons\/((?:aws|cloudflare)\/[a-z0-9-]+)\.svg$/.exec(icon || "");
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
      toneMapped:false,
      uniforms:{
        backdrop:{value:new T.Color()}, surface:{value:new T.Color()},
        grid:{value:new T.Color()}, radius:{value:24}
      },
      vertexShader:`
        varying vec2 ground;
        void main() {
          vec4 world = modelMatrix * vec4(position, 1.0);
          ground = world.xz;
          gl_Position = projectionMatrix * viewMatrix * world;
        }`,
      fragmentShader:`
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
          gl_FragColor = vec4(mix(backdrop, mix(surface, grid, ink), falloff), 1.0);
          #include <colorspace_fragment>
        }`
    });
    const floor=new T.Mesh(new T.PlaneGeometry(2,2),material);
    floor.rotation.x=-Math.PI/2;
    floor.position.y=-1.05;
    return floor;
  }

  function createArtifactTopology({stage,canvas,tooltip,message,selectedLabel,list,search,count,fit,onSelect}) {
    let items=[],byKey=new Map(),selectedKey="",active=false,drawQueued=false,pointer=null;
    let yaw=-.46,pitch=.45,distance=30,target=[0,0,0],contextLost=false,disposed=false;
    let renderer=null,scene=null,camera=null,runtime=null,edges=null,halo=null,environment=null;
    let floor=null,raycaster=null,mouse=null,lastEdgeSignature="";
    const nodes=new Map(),modelCache=new Map(),loadingModels=new Set(),failedModels=new Set();
    const fallbackGeometry=new Map(),fallbackMaterials=new Map();
    const listeners=[];
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

    function setMessage(text) {message.textContent=text;message.hidden=!text;}
    function updateMessage() {
      if(!items.length)setMessage("Select the deployment target to view its artifacts.");
      else if(contextLost)setMessage("3D view is temporarily unavailable. Select an artifact from the list below.");
      else if(!renderer)setMessage("3D view is unavailable here. Select an artifact from the list below.");
      else if(items.some(item=>loadingModels.has(modelPathFor(item.type,window.wizardAWSIconFor))))setMessage("Loading resource models…");
      else if(items.some(item=>failedModels.has(modelPathFor(item.type,window.wizardAWSIconFor))))setMessage("Some resource models could not be loaded. Those artifacts use basic shapes.");
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
        renderer.setPixelRatio(Math.min(window.devicePixelRatio||1,2));
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
        const key=new T.DirectionalLight(0xffead9,1.65);
        key.position.set(-4,7,5);
        const rim=new T.DirectionalLight(0xc3e1ff,2.0);
        rim.position.set(4,5,-6);
        floor=createFloor(T);
        scene.add(key,rim,floor);
        edges=new T.LineSegments(new T.BufferGeometry(),new T.LineBasicMaterial({color:0x61748c}));
        scene.add(edges);
        const haloBox=new T.BoxGeometry(1.95,1.95,1.95),haloEdges=new T.EdgesGeometry(haloBox);haloBox.dispose();
        halo=new T.LineSegments(haloEdges,new T.LineBasicMaterial({color:0xa6c8ff,depthTest:false,transparent:true,opacity:.9}));
        halo.renderOrder=10;halo.visible=false;scene.add(halo);
        lastEdgeSignature="";syncArtifacts();canvas.hidden=false;updateMessage();
        return true;
      } catch {
        releaseRenderer();canvas.hidden=true;updateMessage();return false;
      }
    }
    function releaseRenderer() {
      nodes.clear();
      for(const geometry of fallbackGeometry.values())geometry.dispose();fallbackGeometry.clear();
      for(const material of fallbackMaterials.values())material.dispose();fallbackMaterials.clear();
      edges?.geometry.dispose();edges?.material.dispose();halo?.geometry.dispose();halo?.material.dispose();
      floor?.geometry.dispose();floor?.material.dispose();floor=null;
      environment?.dispose();renderer?.dispose();
      renderer=null;scene=null;camera=null;edges=null;halo=null;environment=null;raycaster=null;mouse=null;
    }
    function fallbackFor(category) {
      const T=runtime.THREE;
      if(!fallbackGeometry.has(category)) {
        const geometry=category==="compute"?new T.ConeGeometry(.95,1.8,4):
          category==="network"?new T.SphereGeometry(.85,16,10):
          category==="security"?new T.OctahedronGeometry(1):
          category==="message"?new T.ConeGeometry(.85,1.8,16):new T.BoxGeometry(1.7,1.7,1.7);
        fallbackGeometry.set(category,geometry);
        fallbackMaterials.set(category,new T.MeshStandardMaterial({color:new T.Color(...categories[category].color),roughness:.5}));
      }
      return new T.Mesh(fallbackGeometry.get(category),fallbackMaterials.get(category));
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
          centered.add(gltf.scene);template.add(centered);template.scale.setScalar(1.7/extent);
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
    function syncArtifacts() {
      if(!scene)return;
      const T=runtime.THREE,keys=new Set(items.map(item=>item.key));
      for(const [key,node] of nodes)if(!keys.has(key)){scene.remove(node.group);nodes.delete(key);}
      for(const item of items) {
        const path=modelPathFor(item.type,window.wizardAWSIconFor);
        let node=nodes.get(item.key);
        if(node&&(node.path!==path||node.category!==item.category)){
          scene.remove(node.group);nodes.delete(item.key);node=null;
        }
        if(!node) {
          const group=new T.Group();group.userData.artifactKey=item.key;
          const visual=fallbackFor(item.category);group.add(visual);scene.add(group);
          node={group,visual,path,category:item.category};nodes.set(item.key,node);
          if(path) {
            const current=node;
            loadModel(path).then(template=>{
              // A refreshed inventory or restored WebGL context may have replaced this node.
              if(!template||disposed||nodes.get(item.key)!==current)return;
              current.group.remove(current.visual);
              current.visual=template.clone(true);current.group.add(current.visual);
              requestDraw();
            });
          }
        }
        node.group.position.set(...item.position);
      }
      // The layout places roots at ground level and descendants above them.
      const radius=items.reduce((r,item)=>Math.max(r,Math.hypot(item.position[0],item.position[2])),0)*1.6+18;
      floor.scale.set(radius,radius,1);
      floor.material.uniforms.radius.value=radius;
      updateVisibility();
    }
    function updateVisibility() {
      if(!scene)return;
      const query=search.value.trim().toLowerCase();
      for(const item of items){const node=nodes.get(item.key);if(node)node.group.visible=matchesArtifact(item,query);}
      const connections=visibleEdges(items,query);
      const signature=JSON.stringify(connections.map(([a,b])=>[a,b,byKey.get(a).position,byKey.get(b).position]));
      if(signature!==lastEdgeSignature) {
        lastEdgeSignature=signature;
        const values=[];for(const [parent,child] of connections)values.push(...byKey.get(parent).position,...byKey.get(child).position);
        const geometry=new runtime.THREE.BufferGeometry();
        geometry.setAttribute("position",new runtime.THREE.Float32BufferAttribute(values,3));
        edges.geometry.dispose();edges.geometry=geometry;
      }
      const selected=byKey.get(selectedKey);
      halo.visible=!!selected&&matchesArtifact(selected,query);
      if(selected)halo.position.set(...selected.position);
    }
    function fitView() {
      const query=search.value.trim().toLowerCase(),visible=items.filter(item=>matchesArtifact(item,query));
      if(!visible.length){target=[0,0,0];distance=30;requestDraw();return;}
      const extents=[0,1,2].map(i=>[Math.min(...visible.map(a=>a.position[i])),Math.max(...visible.map(a=>a.position[i]))]);
      target=extents.map(([min,max])=>(min+max)/2);
      const radius=Math.max(...visible.map(a=>Math.hypot(...subtract(a.position,target))))+2;
      distance=Math.max(12,radius*2.7);requestDraw();
    }
    function draw() {
      drawQueued=false;
      if(disposed||!active||document.hidden||!items.length||contextLost||!initialize())return;
      const bounds=stage.getBoundingClientRect();if(bounds.width<2||bounds.height<2)return;
      renderer.setPixelRatio(Math.min(window.devicePixelRatio||1,2));
      renderer.setSize(bounds.width,bounds.height,false);
      const color=getComputedStyle(stage).backgroundColor;
      scene.background=new runtime.THREE.Color(color);
      const theme=document.documentElement.dataset.theme;
      const lightTheme=theme?theme==="light":systemTheme?.matches;
      const uniforms=floor.material.uniforms;
      uniforms.backdrop.value.copy(scene.background);
      uniforms.surface.value.setHex(lightTheme?0xc2cfda:0x415061);
      uniforms.grid.value.setHex(lightTheme?0xa6b5c3:0x516174);
      edges.material.color.set(lightTheme?0x576c87:0x98adc6);
      camera.aspect=bounds.width/bounds.height;camera.updateProjectionMatrix();
      camera.position.set(target[0]+distance*Math.sin(yaw)*Math.cos(pitch),target[1]+distance*Math.sin(pitch),
        target[2]+distance*Math.cos(yaw)*Math.cos(pitch));
      camera.lookAt(...target);camera.updateMatrixWorld();
      renderer.render(scene,camera);
    }
    function requestDraw() {
      if(disposed||!active||drawQueued||contextLost)return;
      drawQueued=true;requestAnimationFrame(draw);
    }
    function updateSelection() {
      const selected=byKey.get(selectedKey);selectedLabel.hidden=!selected;
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
      if(!byKey.has(selectedKey))selectedKey="";
      canvas.hidden=!items.length||!renderer;syncArtifacts();renderList();
      if(previousStructure!==items.map(a=>a.key+"\0"+a.parent).join("\n"))fitView();else requestDraw();
      if(!items.length||renderer)updateMessage();
    }
    function setSelection(key) {selectedKey=key||"";updateSelection();}
    function activate() {if(disposed)return;active=true;if(items.length)initialize();requestDraw();}
    function deactivate() {active=false;pointer=null;stage.classList.remove("dragging");tooltip.hidden=true;}
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
    listen(stage,"pointerdown",event=>{if(event.button!==0)return;
      pointer={id:event.pointerId,x:event.clientX,y:event.clientY,yaw,pitch,dragged:false};stage.setPointerCapture?.(event.pointerId);tooltip.hidden=true;});
    listen(stage,"pointermove",event=>{
      if(pointer?.id===event.pointerId){const dx=event.clientX-pointer.x,dy=event.clientY-pointer.y;
        if(Math.hypot(dx,dy)>4)pointer.dragged=true;
        if(pointer.dragged){yaw=pointer.yaw+dx*.008;pitch=Math.max(-1.35,Math.min(1.35,pointer.pitch+dy*.008));stage.classList.add("dragging");requestDraw();return;}}
      const item=near(event.clientX,event.clientY);tooltip.hidden=!item;
      if(item){tooltip.replaceChildren();const title=document.createElement("strong"),kind=document.createElement("small");
        title.textContent=item.name;kind.textContent=categories[item.category].label;tooltip.append(title,kind);
        const rect=stage.getBoundingClientRect();tooltip.style.left=Math.max(8,Math.min(event.clientX-rect.left+12,rect.width-270))+"px";
        tooltip.style.top=Math.max(8,Math.min(event.clientY-rect.top+12,rect.height-65))+"px";}});
    listen(stage,"pointerup",event=>{if(pointer?.id!==event.pointerId)return;
      const dragged=pointer.dragged;pointer=null;stage.classList.remove("dragging");if(!dragged){const item=near(event.clientX,event.clientY);if(item)onSelect(item.key);}});
    listen(stage,"pointercancel",()=>{pointer=null;stage.classList.remove("dragging");});
    listen(stage,"pointerleave",()=>{if(!pointer)tooltip.hidden=true;});
    listen(stage,"wheel",event=>{if(!active||!items.length)return;event.preventDefault();
      distance=Math.max(5,Math.min(500,distance*Math.exp(event.deltaY*.001)));requestDraw();},{passive:false});
    listen(stage,"keydown",event=>{let handled=true;
      if(event.key==="ArrowLeft")yaw-=.13;else if(event.key==="ArrowRight")yaw+=.13;
      else if(event.key==="ArrowUp")pitch=Math.min(1.35,pitch+.13);else if(event.key==="ArrowDown")pitch=Math.max(-1.35,pitch-.13);
      else if(event.key==="+"||event.key==="=")distance=Math.max(5,distance*.85);else if(event.key==="-")distance=Math.min(500,distance*1.15);else handled=false;
      if(handled){event.preventDefault();requestDraw();}});
    listen(canvas,"webglcontextlost",event=>{event.preventDefault();contextLost=true;canvas.hidden=true;updateMessage();});
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

  if(typeof window!=="undefined")window.createArtifactTopology=createArtifactTopology;
  if(typeof module!=="undefined"&&module.exports)module.exports={categoryFor,layoutArtifacts,perspective,lookAt,multiply,project,modelPathFor,matchesArtifact,visibleEdges};
})();
