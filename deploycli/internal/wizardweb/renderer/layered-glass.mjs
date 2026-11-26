// Three's transmission pass samples opaque geometry. Resolve overlapping pieces
// back to front, presenting already shaded glass as opaque geometry to that pass.
import {
  DoubleSide, HalfFloatType, LinearFilter, MeshBasicMaterial, ShaderMaterial,
  UnsignedByteType, Vector2, Vector3, WebGLRenderTarget
} from "three";

export class LayeredGlass {
  constructor(renderer) {
    this.renderer = renderer;
    this.targets = [];
    this.size = new Vector2();
    this.point = new Vector3();
    this.shadowOnly = new MeshBasicMaterial({colorWrite:false, depthWrite:false});
    this.resolved = new ShaderMaterial({
      side:DoubleSide, uniforms:{backdrop:{value:null}},
      vertexShader:`
        varying vec4 clipPosition;
        void main() {
          clipPosition = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
          gl_Position = clipPosition;
        }`,
      fragmentShader:`
        uniform sampler2D backdrop;
        varying vec4 clipPosition;
        void main() {
          gl_FragColor = texture2D(backdrop, clipPosition.xy / clipPosition.w * 0.5 + 0.5);
          #include <tonemapping_fragment>
          #include <colorspace_fragment>
        }`
    });
  }

  footprint(mesh, camera) {
    const geometry = mesh.geometry;
    if (!geometry.boundingBox) geometry.computeBoundingBox();
    const {min,max} = geometry.boundingBox;
    let left=Infinity, right=-Infinity, top=-Infinity, bottom=Infinity;
    let near=Infinity, far=-Infinity;
    for (let i=0; i<8; i++) {
      const p=this.point.set(i&1 ? max.x:min.x, i&2 ? max.y:min.y, i&4 ? max.z:min.z);
      p.applyMatrix4(mesh.matrixWorld).applyMatrix4(camera.matrixWorldInverse);
      near=Math.min(near,-p.z); far=Math.max(far,-p.z);
      p.applyMatrix4(camera.projectionMatrix);
      left=Math.min(left,p.x); right=Math.max(right,p.x);
      bottom=Math.min(bottom,p.y); top=Math.max(top,p.y);
    }
    if (far < camera.near || near > camera.far) return null;
    if (near < camera.near) { left=bottom=-1; right=top=1; }
    if (left>1 || right< -1 || bottom>1 || top< -1) return null;
    return {left,right,bottom,top,near,far};
  }

  render(scene, camera, nodes) {
    const pieces=[];
    for (const {group,visual} of nodes) {
      if (!group.visible) continue;
      group.updateWorldMatrix(true,true);
      const depth=-this.point.setFromMatrixPosition(group.matrixWorld).applyMatrix4(camera.matrixWorldInverse).z;
      visual.traverseVisible(mesh=>{
        if (!mesh.isMesh || !(mesh.material.transmission>0)) return;
        const bounds=this.footprint(mesh,camera);
        if (bounds) pieces.push({...bounds,group,depth,mesh,material:mesh.material,layer:0});
      });
    }
    pieces.sort((a,b)=>b.depth-a.depth || a.group.id-b.group.id || (b.far+b.near)-(a.far+a.near));
    let last=0;
    for (let i=0;i<pieces.length;i++) {
      const front=pieces[i];
      for (let j=0;j<i;j++) {
        const back=pieces[j];
        // Intersecting details within one model share Three's regular depth test.
        if (back.group===front.group && back.near<front.far) continue;
        if (back.right<=front.left || front.right<=back.left || back.top<=front.bottom || front.top<=back.bottom) continue;
        front.layer=Math.max(front.layer, back.layer+1);
      }
      last=Math.max(last,front.layer);
    }
    const renderer=this.renderer;
    if (!last) { renderer.render(scene,camera); return; }
    renderer.getDrawingBufferSize(this.size);
    const destination=renderer.getRenderTarget();
    let previous=null;
    try {
      for (let layer=0;layer<=last;layer++) {
        this.resolved.uniforms.backdrop.value=previous?.texture ?? null;
        for (const piece of pieces) {
          piece.mesh.material=piece.layer===layer ? piece.material :
            piece.layer<layer ? this.resolved : this.shadowOnly;
        }
        const output=layer===last ? destination : this.target(layer%2);
        renderer.setRenderTarget(output);
        renderer.render(scene,camera);
        previous=output;
      }
    } finally {
      for (const piece of pieces) piece.mesh.material=piece.material;
      this.resolved.uniforms.backdrop.value=null;
      renderer.setRenderTarget(destination);
    }
  }

  target(index) {
    let target=this.targets[index];
    if (!target) {
      const type=this.renderer.extensions.has("EXT_color_buffer_float") ? HalfFloatType : UnsignedByteType;
      target=new WebGLRenderTarget(1,1,{type,minFilter:LinearFilter,magFilter:LinearFilter});
      this.targets[index]=target;
    }
    target.setSize(this.size.x,this.size.y);
    return target;
  }

  dispose() {
    for (const target of this.targets) target.dispose();
    this.resolved.dispose();
    this.shadowOnly.dispose();
  }
}
