// Route the centerline in world space. Inflating every obstacle by the largest
// cross-section protects the whole mesh, including the beveled arrow shoulders.
import {
  Box3, BufferGeometry, CubicBezierCurve3, Curve, Float32BufferAttribute,
  Group, Mesh, MeshPhysicalMaterial, QuadraticBezierCurve3, TubeGeometry, Vector3
} from "three";

const radius = .027, headWidth = 6 * radius, headLength = .20;
const gap = .12, clearance = gap + headWidth * .58;
const axes = ["x","y","z"], up = new Vector3(0,1,0);

function crosses(a,b,box) {
  let lo=0, hi=1;
  for (const axis of axes) {
    const delta=b[axis]-a[axis];
    if (Math.abs(delta)<1e-10) {
      if (a[axis]<box.min[axis] || a[axis]>box.max[axis]) return false;
    } else {
      const t0=(box.min[axis]-a[axis])/delta, t1=(box.max[axis]-a[axis])/delta;
      lo=Math.max(lo,Math.min(t0,t1)); hi=Math.min(hi,Math.max(t0,t1));
      if (lo>hi) return false;
    }
  }
  return true;
}

function clear(points,boxes) {
  return points.every((p,i)=>i===0 || !boxes.some(box=>crosses(points[i-1],p,box)));
}

// Choose the face hit by the center-to-center ray. Spread sibling ports over
// that face; a short normal at each end gives the conduit its natural bend.
function port(box,toward) {
  const center=box.getCenter(new Vector3()), half=box.getSize(new Vector3()).multiplyScalar(.5);
  const delta=toward.clone().sub(center);
  let axis="x";
  for (const k of axes) if (Math.abs(delta[k])/half[k]>Math.abs(delta[axis])/half[axis]) axis=k;
  const sign=Math.sign(delta[axis]) || 1;
  const point=center.addScaledVector(delta,.86*half[axis]/Math.max(1e-9,Math.abs(delta[axis])));
  point[axis]=(sign>0?box.max:box.min)[axis]+sign*.012;
  const normal=new Vector3(); normal[axis]=sign;
  return {point,normal};
}

// Shortest visible path around box corners. Grow this visibility graph only
// when the current route encounters another box, avoiding an all-scene graph
// for the common unobstructed parent/child relationship.
function detour(start,end,boxes) {
  const known=new Set();
  let route=[start,end];
  for (;;) {
    const hit=boxes.filter(box=>!known.has(box) && !clear(route,[box]));
    if (!hit.length) return route;
    hit.forEach(box=>known.add(box));
    const obstacles=[...known], vertices=[start,end];
    for (const obstacle of obstacles) {
      const b=obstacle.clone().expandByScalar(.15);
      for (let i=0;i<8;i++) vertices.push(new Vector3(
        (i&1?b.max:b.min).x,(i&2?b.max:b.min).y,(i&4?b.max:b.min).z));
    }
    const cost=vertices.map(()=>Infinity), prev=[], done=new Set();
    cost[0]=0;
    for (;;) {
      let best=-1;
      for (let i=0;i<vertices.length;i++) {
        if (!done.has(i) && Number.isFinite(cost[i]) && (best<0 || cost[i]<cost[best])) best=i;
      }
      if (best<0) return null;
      if (best===1) break;
      done.add(best);
      for (let i=1;i<vertices.length;i++) {
        const next=cost[best]+vertices[best].distanceTo(vertices[i]);
        if (!done.has(i) && next<cost[i] && clear([vertices[best],vertices[i]],obstacles)) {
          cost[i]=next; prev[i]=best;
        }
      }
    }
    route=[];
    for (let i=1;i!==undefined;i=prev[i]) route.unshift(vertices[i]);
  }
}

function rounded(points,boxes) {
  const path=[points[0]];
  for (let i=1;i<points.length-1;i++) {
    const p=points[i], before=points[i-1].clone().sub(p), after=points[i+1].clone().sub(p);
    let cut=Math.min(.55,before.length()*.4,after.length()*.4);
    before.normalize(); after.normalize();
    for (;;) {
      const arc=new QuadraticBezierCurve3(p.clone().addScaledVector(before,cut),p,
        p.clone().addScaledVector(after,cut)).getPoints(10);
      if (clear(arc,boxes)) {path.push(...arc); break;}
      cut*=.5;
      if (cut<.0001) {path.push(p); break;}
    }
  }
  path.push(points.at(-1));
  return path;
}

function route(source,destination,boxes) {
  const a=port(source,destination.getCenter(new Vector3()));
  const b=port(destination,source.getCenter(new Vector3()));
  const length=Math.min(headLength,a.point.distanceTo(b.point)*.22);
  const base=b.point.clone().addScaledVector(b.normal,length);
  if (!clear([base,b.point],boxes)) return null;
  const bend=Math.min(.64,a.point.distanceTo(base)*.36);
  const lead=base.clone().addScaledVector(b.normal,Math.min(.06,bend*.22));
  const curve=new CubicBezierCurve3(a.point,a.point.clone().addScaledVector(a.normal,bend),
    lead.clone().addScaledVector(b.normal,bend),lead);
  let points=[...curve.getPoints(32),base];
  if (!clear(points,boxes)) {
    // A straight terminal run makes the arrow axis exactly the final tangent.
    const via=detour(a.point,lead,boxes);
    if (!via || !clear([lead,base],boxes)) return null;
    points=rounded([...via,base],boxes);
  }
  return {points,tip:b.point,direction:b.normal.negate(),length};
}

// TubeGeometry samples at these exact knots. Collision checks therefore cover
// the actual chords between rendered rings, even around a rounded detour.
class SampledPath extends Curve {
  constructor(points) {super(); this.points=points;}
  getPointAt(t,target=new Vector3()) {
    const last=this.points.length-1, f=t*last, i=Math.min(Math.floor(f),last-1);
    return target.lerpVectors(this.points[i],this.points[i+1],f-i);
  }
  getTangentAt(t,target=new Vector3()) {
    const i=Math.round(t*(this.points.length-1));
    return target.subVectors(this.points[Math.min(i+1,this.points.length-1)],this.points[Math.max(0,i-1)]).normalize();
  }
}

function tubeGeometry(points) {
  const sides=10, last=(points.length-1)*(sides+1);
  const geometry=new TubeGeometry(new SampledPath(points),points.length-1,radius,sides,false);
  const indices=Array.from(geometry.index.array);
  for (let i=1;i<sides-1;i++) indices.push(0,i,i+1,last,last+i+1,last+i);
  geometry.setIndex(indices);
  return geometry;
}

function arrowGeometry() {
  // Three clipped corners per cross-section and a narrow rear bevel catch
  // reflections from any viewing angle. Local +Y ends at the tip.
  const vertices=[], indices=[];
  const corners=Array.from({length:3},(_,i)=>new Vector3(Math.cos(i*2*Math.PI/3),0,Math.sin(i*2*Math.PI/3)));
  for (const [y,r] of [[-1,.86],[-.94,1],[-.075,.085]]) {
    for (let i=0;i<3;i++) for (const j of [2,1]) {
      const p=corners[i].clone().lerp(corners[(i+j)%3],.065);
      vertices.push(p.x*r,y,p.z*r);
    }
  }
  for (let j=0;j<2;j++) for(let i=0;i<6;i++) {
    const a=j*6+i,b=j*6+(i+1)%6;
    indices.push(a,a+6,b,b,a+6,b+6);
  }
  vertices.push(0,0,0);
  for(let i=0;i<6;i++) indices.push(12+i,18,12+(i+1)%6);
  for(let i=1;i<5;i++) indices.push(0,i,i+1);
  const geometry=new BufferGeometry();
  geometry.setAttribute("position",new Float32BufferAttribute(vertices,3));
  geometry.setIndex(indices);
  const faceted=geometry.toNonIndexed(); geometry.dispose();
  faceted.computeVertexNormals();
  return faceted;
}

export class DirectedConduits {
  constructor() {
    this.group=new Group();
    this.group.name="Directed glass conduits";
    this.material=new MeshPhysicalMaterial({color:0xfff4e2,roughness:.085,transmission:.96,transparent:true,opacity:.68,
      ior:1.46,thickness:.038,attenuationColor:0xffbf74,attenuationDistance:.22,clearcoat:.3});
    this.arrow=arrowGeometry();
  }

  update(nodes,connections) {
    this.clear();
    const bounds=new Map();
    for (const [key,node] of nodes) if(node.group.visible) {
      node.visual.updateWorldMatrix(true,true);
      bounds.set(key,new Box3().setFromObject(node.visual).expandByScalar(clearance));
    }
    const boxes=[...bounds.values()];
    for (const [source,destination] of connections) {
      if (source===destination) continue;
      const path=route(bounds.get(source),bounds.get(destination),boxes);
      if (!path) continue;
      const group=new Group();
      group.userData={source,destination};
      const tube=new Mesh(tubeGeometry(path.points),this.material);
      const head=new Mesh(this.arrow,this.material);
      head.scale.set(headWidth/Math.sqrt(3),path.length,headWidth/Math.sqrt(3));
      head.position.copy(path.tip); head.quaternion.setFromUnitVectors(up,path.direction);
      group.add(tube,head); this.group.add(group);
    }
  }

  clear() {
    for (const edge of this.group.children) edge.children[0].geometry.dispose();
    this.group.clear();
  }
  dispose() {this.clear(); this.material.dispose(); this.arrow.dispose();}
}
