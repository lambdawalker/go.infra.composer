// Blend complete images rather than fading individual glass surfaces. This
// keeps a multi-part artifact at one strength, including its shadow and refraction.
import {FramebufferTexture,Mesh,OrthographicCamera,PlaneGeometry,Scene,ShaderMaterial,Vector2} from "three";

export class SelectionFocus {
  constructor(renderer) {
    this.renderer=renderer;
    this.size=new Vector2();
    this.scene=new Scene();
    this.scene.name="Selection focus overlay";
    this.camera=new OrthographicCamera(-1,1,1,-1,0,1);
    this.quad=new Mesh(new PlaneGeometry(2,2),new ShaderMaterial({
      uniforms:{complete:{value:null}},transparent:true,depthTest:false,depthWrite:false,toneMapped:false,
      vertexShader:"varying vec2 point; void main(){point=uv; gl_Position=vec4(position.xy,0.0,1.0);}",
      // The framebuffer already contains display-encoded colors.
      fragmentShader:"varying vec2 point; uniform sampler2D complete; void main(){gl_FragColor=vec4(texture2D(complete,point).rgb,0.35);}"
    }));
    this.scene.add(this.quad);
  }

  render(context,drawScene) {
    const renderer=this.renderer;
    drawScene();
    if(!context.length)return;
    renderer.getDrawingBufferSize(this.size);
    if(!this.texture || this.texture.image.width!==this.size.x || this.texture.image.height!==this.size.y){
      this.texture?.dispose();
      this.texture=new FramebufferTexture(this.size.x,this.size.y);
      this.quad.material.uniforms.complete.value=this.texture;
    }
    renderer.copyFramebufferToTexture(this.texture);
    const autoClear=renderer.autoClear;
    try {
      for(const object of context)object.visible=false;
      renderer.shadowMap.needsUpdate=true;
      drawScene();
      renderer.autoClear=false;
      renderer.render(this.scene,this.camera);
    } finally {
      for(const object of context)object.visible=true;
      renderer.autoClear=autoClear;
      renderer.shadowMap.needsUpdate=true;
    }
  }

  dispose() {
    this.texture?.dispose();
    this.quad.material.dispose();
    this.quad.geometry.dispose();
  }
}
