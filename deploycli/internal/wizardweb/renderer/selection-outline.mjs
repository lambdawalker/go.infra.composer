// Red stores the full selected silhouette. Green stores its visible portion.
// Sampling both avoids drawing an artificial edge across a foreground artifact.
import {
  Color, CustomBlending, DoubleSide, Mesh, MeshBasicMaterial, OrthographicCamera,
  PlaneGeometry, Scene, ShaderMaterial, SrcColorFactor, Vector2, WebGLRenderTarget,
  ZeroFactor
} from "three";

const maskLayer = 30;

export class SelectionOutline {
  constructor(renderer) {
    this.renderer = renderer;
    this.size = new Vector2();
    this.clearColor = new Color();
    this.target = new WebGLRenderTarget(1, 1);
    this.target.samples = Math.min(4, renderer.capabilities.maxSamples || 0);
    this.silhouette = new MeshBasicMaterial({color:0xffffff, side:DoubleSide, toneMapped:false, fog:false});
    this.occluder = new MeshBasicMaterial({
      color:0xff0000, side:DoubleSide, toneMapped:false, fog:false, depthWrite:false,
      blending:CustomBlending, blendSrc:ZeroFactor, blendDst:SrcColorFactor
    });
    this.overlay = new Scene();
    this.overlay.name = "Selection outline overlay";
    this.camera = new OrthographicCamera(-1, 1, 1, -1, 0, 1);
    this.quad = new Mesh(new PlaneGeometry(2, 2), new ShaderMaterial({
      transparent:true, depthTest:false, depthWrite:false,
      uniforms:{mask:{value:this.target.texture}, pixel:{value:new Vector2()}},
      vertexShader:`
        varying vec2 point;
        void main() { point = uv; gl_Position = vec4(position.xy, 0.0, 1.0); }`,
      fragmentShader:`
        varying vec2 point;
        uniform sampler2D mask;
        uniform vec2 pixel;
        void main() {
          float outside = 1.0 - texture2D(mask, point).r;
          if (outside <= 0.0) discard;
          float edge = 0.0, halo = 0.0;
          for (int i = 0; i < 12; i++) {
            float angle = float(i) * 0.52359878;
            vec2 stepUV = pixel * vec2(cos(angle), sin(angle));
            edge = max(edge, texture2D(mask, point + 1.2 * stepUV).g);
            halo += texture2D(mask, point + 3.1 * stepUV).g;
          }
          gl_FragColor = vec4(mix(vec3(.22,.48,.77), vec3(.57,.81,1.), edge),
                              outside * min(1.0, .94 * edge + .033 * halo));
          #include <colorspace_fragment>
        }`
    }));
    this.overlay.add(this.quad);
  }

  render(scene, camera, visual, width, height) {
    if (!visual) return;
    const renderer = this.renderer;
    const destination = renderer.getRenderTarget();
    const background = scene.background;
    const autoClear = renderer.autoClear;
    const clearAlpha = renderer.getClearAlpha();
    renderer.getClearColor(this.clearColor);
    const cameraMask = camera.layers.mask;
    const selected = new Set();
    visual.traverseVisible(object => { if (object.isMesh && object.material.visible!==false) selected.add(object); });
    if (!selected.size) return;
    const originals = [];
    renderer.getDrawingBufferSize(this.size);
    this.target.setSize(this.size.x, this.size.y);
    try {
      // Substitute cheap solid materials so transmissive glass counts as a
      // continuous silhouette and does not trigger another refraction pass.
      scene.traverseVisible(object => {
        if (!object.isMesh || object.material.visible===false) return;
        originals.push([object, object.material, object.layers.mask]);
        if (selected.has(object)) {
          object.layers.set(maskLayer);
          object.material = this.silhouette;
        } else {
          object.material = this.occluder;
        }
      });
      scene.background = null;
      renderer.autoClear = false;
      renderer.setRenderTarget(this.target);
      renderer.setClearColor(0, 1);
      renderer.clear();
      camera.layers.set(maskLayer);
      renderer.render(scene, camera);
      camera.layers.mask = cameraMask;
      renderer.render(scene, camera);

      renderer.setRenderTarget(destination);
      this.quad.material.uniforms.pixel.value.set(1 / width, 1 / height);
      renderer.render(this.overlay, this.camera);
    } finally {
      for (const [object, material, layers] of originals) {
        object.material = material;
        object.layers.mask = layers;
      }
      camera.layers.mask = cameraMask;
      scene.background = background;
      renderer.autoClear = autoClear;
      renderer.setClearColor(this.clearColor, clearAlpha);
      renderer.setRenderTarget(destination);
    }
  }

  dispose() {
    this.target.dispose();
    this.quad.geometry.dispose();
    this.quad.material.dispose();
    this.silhouette.dispose();
    this.occluder.dispose();
  }
}
