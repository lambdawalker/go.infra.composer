"use strict";

// A small, dependency-free WebGL renderer. All labels and controls remain DOM
// elements so the inventory is usable without WebGL and with a keyboard.
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
    if (/dynamodb|s3\/|storage|:Bucket$|:Table$/i.test(type)) return "storage";
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

  function mesh(faces) {
    const data=[];
    for(const [a,b,c] of faces) {
      const n=normal(cross(subtract(b,a),subtract(c,a)));
      for(const v of [a,b,c])data.push(...v,...n);
    }
    return new Float32Array(data);
  }
  function shapes() {
    const top=[0,1.15,0],bottom=[0,-1.15,0];
    const square=[[-.85,-.85,-.85],[.85,-.85,-.85],[.85,-.85,.85],[-.85,-.85,.85]];
    const pyramid=mesh([...square.map((v,i)=>[v,square[(i+1)%4],top]),
      [square[2],square[1],square[0]],[square[0],square[3],square[2]]]);
    const corners=[];for(const y of [-.85,.85])for(const z of [-.85,.85])for(const x of [-.85,.85])corners.push([x,y,z]);
    const cubeFaces=[];
    for(const [a,b,c,d] of [[0,1,3,2],[4,6,7,5],[0,4,5,1],[2,3,7,6],[0,2,6,4],[1,5,7,3]])
      cubeFaces.push([corners[a],corners[b],corners[c]],[corners[a],corners[c],corners[d]]);
    const diamondFaces=[],ring=[[1,0,0],[0,0,1],[-1,0,0],[0,0,-1]];
    for(let i=0;i<4;i++)diamondFaces.push([ring[i],ring[(i+1)%4],top],[ring[(i+1)%4],ring[i],bottom]);
    const coneFaces=[],sphereFaces=[],segments=16,latitudes=10;
    for(let i=0;i<segments;i++){
      const angle=i*2*Math.PI/segments,next=(i+1)*2*Math.PI/segments;
      const a=[Math.cos(angle),-.95,Math.sin(angle)],b=[Math.cos(next),-.95,Math.sin(next)];
      coneFaces.push([a,b,top],[b,a,[0,-.95,0]]);
    }
    function spherePoint(lat,lon) {const a=lat*Math.PI/latitudes,b=lon*2*Math.PI/segments;
      return [Math.sin(a)*Math.cos(b),Math.cos(a),Math.sin(a)*Math.sin(b)];}
    for(let lat=0;lat<latitudes;lat++)for(let lon=0;lon<segments;lon++){
      const a=spherePoint(lat,lon),b=spherePoint(lat+1,lon),c=spherePoint(lat+1,lon+1),d=spherePoint(lat,lon+1);
      if(lat>0)sphereFaces.push([a,b,d]);
      if(lat<latitudes-1)sphereFaces.push([b,c,d]);
    }
    return {pyramid,cube:mesh(cubeFaces),diamond:mesh(diamondFaces),cone:mesh(coneFaces),sphere:mesh(sphereFaces)};
  }

  function createArtifactTopology({stage,canvas,tooltip,message,selectedLabel,list,search,count,fit,onSelect}) {
    let items=[],byKey=new Map(),selectedKey="",active=false,gl=null,gpu=null,drawQueued=false;
    let yaw=.55,pitch=.45,distance=30,target=[0,0,0],lastMatrix=null,pointer=null;
    const geometry=shapes();
    const resizeObserver=typeof ResizeObserver!=="undefined"?new ResizeObserver(()=>requestDraw()):null;
    resizeObserver?.observe(stage);
    window.addEventListener("resize",()=>requestDraw());
    document.addEventListener("visibilitychange",()=>{if(!document.hidden)requestDraw();});
    new MutationObserver(()=>requestDraw()).observe(document.documentElement,{attributes:true,attributeFilter:["data-theme"]});

    function setMessage(text) {message.textContent=text;message.hidden=!text;}
    function shader(type,source) {
      const object=gl.createShader(type);gl.shaderSource(object,source);gl.compileShader(object);
      if(!gl.getShaderParameter(object,gl.COMPILE_STATUS)) {gl.deleteShader(object);throw Error("Shader unavailable");}
      return object;
    }
    function initialize() {
      if(gl)return true;
      try {
        gl=canvas.getContext("webgl",{antialias:true,alpha:false});
        if(!gl)throw Error("WebGL unavailable");
        const vertex=shader(gl.VERTEX_SHADER,`attribute vec3 position; attribute vec3 normal;
          uniform mat4 viewProjection; uniform vec3 center; uniform float scale;
          varying float light; void main() { gl_Position=viewProjection*vec4(position*scale+center,1.0);
          light=.48+.52*max(dot(normalize(normal),normalize(vec3(.35,.9,.55))),0.0); }`);
        const fragment=shader(gl.FRAGMENT_SHADER,`precision mediump float; uniform vec3 color;
          varying float light; void main() { gl_FragColor=vec4(color*light,1.0); }`);
        const program=gl.createProgram();gl.attachShader(program,vertex);gl.attachShader(program,fragment);gl.linkProgram(program);
        gl.deleteShader(vertex);gl.deleteShader(fragment);
        if(!gl.getProgramParameter(program,gl.LINK_STATUS))throw Error("Shader program unavailable");
        gl.useProgram(program);
        gpu={program,position:gl.getAttribLocation(program,"position"),normal:gl.getAttribLocation(program,"normal"),
          viewProjection:gl.getUniformLocation(program,"viewProjection"),center:gl.getUniformLocation(program,"center"),
          scale:gl.getUniformLocation(program,"scale"),color:gl.getUniformLocation(program,"color"),buffers:{},edges:gl.createBuffer(),edgeVertices:0};
        for(const [kind,vertices] of Object.entries(geometry)){
          const buffer=gl.createBuffer();gl.bindBuffer(gl.ARRAY_BUFFER,buffer);
          gl.bufferData(gl.ARRAY_BUFFER,vertices,gl.STATIC_DRAW);gpu.buffers[kind]={buffer,count:vertices.length/6};
        }
        updateEdges();canvas.hidden=false;
        setMessage(items.length?"":"Select the deployment target to view its artifacts.");
        return true;
      } catch {
        gl=null;gpu=null;canvas.hidden=true;
        setMessage(items.length?"3D view is unavailable here. Select an artifact from the list below.":"Select the deployment target to view its artifacts.");
        return false;
      }
    }
    function updateEdges() {
      if(!gl||!gpu)return;
      const vertices=[];
      for(const item of items){const parent=byKey.get(item.parent);if(!parent)continue;
        for(const p of [parent.position,item.position])vertices.push(...p,0,1,0);}
      gl.bindBuffer(gl.ARRAY_BUFFER,gpu.edges);gl.bufferData(gl.ARRAY_BUFFER,new Float32Array(vertices),gl.DYNAMIC_DRAW);
      gpu.edgeVertices=vertices.length/6;
    }
    function bind(buffer) {
      gl.bindBuffer(gl.ARRAY_BUFFER,buffer);
      gl.enableVertexAttribArray(gpu.position);gl.enableVertexAttribArray(gpu.normal);
      gl.vertexAttribPointer(gpu.position,3,gl.FLOAT,false,24,0);
      gl.vertexAttribPointer(gpu.normal,3,gl.FLOAT,false,24,12);
    }
    function fitView() {
      if(!items.length){target=[0,0,0];distance=30;requestDraw();return;}
      const extents=[0,1,2].map(i=>[Math.min(...items.map(a=>a.position[i])),Math.max(...items.map(a=>a.position[i]))]);
      target=extents.map(([min,max])=>(min+max)/2);
      const radius=Math.max(...items.map(a=>Math.hypot(...subtract(a.position,target))))+2;
      distance=Math.max(12,radius*2.7);
      requestDraw();
    }
    function draw() {
      drawQueued=false;
      if(!active||document.hidden||!items.length||!initialize())return;
      const bounds=stage.getBoundingClientRect();if(bounds.width<2||bounds.height<2)return;
      const ratio=Math.min(window.devicePixelRatio||1,2),width=Math.round(bounds.width*ratio),height=Math.round(bounds.height*ratio);
      if(canvas.width!==width||canvas.height!==height){canvas.width=width;canvas.height=height;}
      gl.viewport(0,0,width,height);
      const background=getComputedStyle(stage).backgroundColor.match(/[\d.]+/g)?.slice(0,3).map(x=>Number(x)/255)||[.12,.12,.13];
      gl.clearColor(...background,1);gl.clear(gl.COLOR_BUFFER_BIT|gl.DEPTH_BUFFER_BIT);gl.enable(gl.DEPTH_TEST);
      const eye=[target[0]+distance*Math.sin(yaw)*Math.cos(pitch),target[1]+distance*Math.sin(pitch),
        target[2]+distance*Math.cos(yaw)*Math.cos(pitch)];
      lastMatrix=multiply(perspective(width/height),lookAt(eye,target));
      gl.useProgram(gpu.program);gl.uniformMatrix4fv(gpu.viewProjection,false,lastMatrix);
      if(gpu.edgeVertices){bind(gpu.edges);gl.uniform3f(gpu.center,0,0,0);gl.uniform1f(gpu.scale,1);
        gl.uniform3f(gpu.color,...(document.documentElement.dataset.theme==="light"?[.55,.61,.7]:[.35,.43,.55]));
        gl.drawArrays(gl.LINES,0,gpu.edgeVertices);}
      const query=search.value.trim().toLowerCase();
      for(const item of items){const info=categories[item.category],chosen=item.key===selectedKey;
        const matching=!query||item.name.toLowerCase().includes(query)||item.type.toLowerCase().includes(query);
        const shape=gpu.buffers[info.shape];bind(shape.buffer);
        gl.uniform3fv(gpu.center,item.position);gl.uniform1f(gpu.scale,chosen?1.35:.82);
        const color=matching?info.color:[.35,.36,.39];
        gl.uniform3f(gpu.color,...(chosen?color.map(v=>Math.min(1,v*1.25)):color));
        gl.drawArrays(gl.TRIANGLES,0,shape.count);
      }
    }
    function requestDraw() {
      if(!active||drawQueued)return;
      drawQueued=true;requestAnimationFrame(draw);
    }
    function updateSelection() {
      const selected=byKey.get(selectedKey);
      selectedLabel.hidden=!selected;
      if(selected)selectedLabel.textContent=`${selected.name} · ${categories[selected.category].label}`;
      for(const button of list.querySelectorAll("button"))button.setAttribute("aria-current",String(button.dataset.key===selectedKey));
      requestDraw();
    }
    function renderList() {
      const query=search.value.trim().toLowerCase();list.replaceChildren();
      const visible=items.filter(a=>!query||a.name.toLowerCase().includes(query)||a.type.toLowerCase().includes(query));
      count.textContent=items.length?`${visible.length} of ${items.length}`:"0";
      if(items.length && !visible.length){const empty=document.createElement("li");empty.textContent="No artifacts match this search.";empty.className="topology-empty";list.append(empty);}
      for(const item of visible){const li=document.createElement("li"),button=document.createElement("button");
        button.type="button";button.dataset.key=item.key;button.title=item.name;
        button.setAttribute("aria-label",`${item.name}, ${categories[item.category].label}, ${item.action}`);
        const marker=document.createElement("i");marker.className="topology-key "+item.category;marker.setAttribute("aria-hidden","true");
        const name=document.createElement("span");name.className="topology-list-text";name.textContent=item.name;
        const kind=document.createElement("span");kind.className="topology-list-kind";kind.textContent=categories[item.category].label;
        button.append(marker,name,kind);button.onclick=()=>onSelect(item.key);
        li.append(button);list.append(li);}
      updateSelection();
    }
    function setArtifacts(artifacts) {
      const previousStructure=items.map(a=>a.key+"\0"+a.parent).join("\n");
      items=layoutArtifacts(artifacts);byKey=new Map(items.map(a=>[a.key,a]));
      if(!byKey.has(selectedKey))selectedKey="";
      canvas.hidden=!items.length || !gl;
      updateEdges();renderList();
      if(previousStructure!==items.map(a=>a.key+"\0"+a.parent).join("\n"))fitView();
      else requestDraw();
      if(!items.length)setMessage("Select the deployment target to view its artifacts.");
      else if(gl)setMessage("");
    }
    function setSelection(key) {selectedKey=key||"";updateSelection();}
    function activate() {active=true;if(items.length)initialize();requestDraw();}
    function deactivate() {active=false;tooltip.hidden=true;}
    function near(clientX,clientY) {
      if(!active||!gl||!lastMatrix)return null;
      const rect=stage.getBoundingClientRect(),x=clientX-rect.left,y=clientY-rect.top;
      let closest=null,best=26*26;
      for(const item of items){const point=project(item.position,lastMatrix,rect.width,rect.height);
        if(!point?.visible)continue;
        const distance=(point.x-x)**2+(point.y-y)**2;
        if(distance<best){best=distance;closest=item;}}
      return closest;
    }
    stage.addEventListener("pointerdown",event=>{if(event.button!==0)return;
      pointer={id:event.pointerId,x:event.clientX,y:event.clientY,yaw,pitch,dragged:false};
      stage.setPointerCapture?.(event.pointerId);tooltip.hidden=true;});
    stage.addEventListener("pointermove",event=>{
      if(pointer?.id===event.pointerId){const dx=event.clientX-pointer.x,dy=event.clientY-pointer.y;
        if(Math.hypot(dx,dy)>4)pointer.dragged=true;
        if(pointer.dragged){yaw=pointer.yaw+dx*.008;pitch=Math.max(-1.35,Math.min(1.35,pointer.pitch+dy*.008));
          stage.classList.add("dragging");requestDraw();return;}}
      const item=near(event.clientX,event.clientY);tooltip.hidden=!item;
      if(item){tooltip.replaceChildren();const title=document.createElement("strong"),kind=document.createElement("small");
        title.textContent=item.name;kind.textContent=categories[item.category].label;tooltip.append(title,kind);
        const rect=stage.getBoundingClientRect();tooltip.style.left=Math.max(8,Math.min(event.clientX-rect.left+12,rect.width-270))+"px";
        tooltip.style.top=Math.max(8,Math.min(event.clientY-rect.top+12,rect.height-65))+"px";}
    });
    stage.addEventListener("pointerup",event=>{if(pointer?.id!==event.pointerId)return;
      const dragged=pointer.dragged;pointer=null;stage.classList.remove("dragging");
      if(!dragged){const item=near(event.clientX,event.clientY);if(item)onSelect(item.key);}});
    stage.addEventListener("pointercancel",()=>{pointer=null;stage.classList.remove("dragging");});
    stage.addEventListener("pointerleave",()=>{if(!pointer)tooltip.hidden=true;});
    stage.addEventListener("wheel",event=>{if(!active||!items.length)return;event.preventDefault();
      distance=Math.max(5,Math.min(500,distance*Math.exp(event.deltaY*.001)));requestDraw();},{passive:false});
    stage.addEventListener("keydown",event=>{let handled=true;
      if(event.key==="ArrowLeft")yaw-=.13;
      else if(event.key==="ArrowRight")yaw+=.13;
      else if(event.key==="ArrowUp")pitch=Math.min(1.35,pitch+.13);
      else if(event.key==="ArrowDown")pitch=Math.max(-1.35,pitch-.13);
      else if(event.key==="+"||event.key==="=")distance=Math.max(5,distance*.85);
      else if(event.key==="-")distance=Math.min(500,distance*1.15);
      else handled=false;
      if(handled){event.preventDefault();requestDraw();}});
    canvas.addEventListener("webglcontextlost",event=>{event.preventDefault();gl=null;gpu=null;
      canvas.hidden=true;
      setMessage("3D view is temporarily unavailable. Select an artifact from the list below.");});
    canvas.addEventListener("webglcontextrestored",()=>{gl=null;gpu=null;if(active)initialize();requestDraw();});
    search.addEventListener("input",()=>{renderList();requestDraw();});
    fit.addEventListener("click",fitView);
    return {setArtifacts,setSelection,activate,deactivate,fitView};
  }

  if(typeof window!=="undefined")window.createArtifactTopology=createArtifactTopology;
  if(typeof module!=="undefined"&&module.exports)module.exports={categoryFor,layoutArtifacts,perspective,lookAt,multiply,project};
})();
