"use strict";
const assert = require("node:assert/strict");
const test = require("node:test");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

function checkSenderCertificate(meshes, THREE) {
  const plaque = meshes.find(mesh => mesh.userData.name === "Engraved glass plaque");
  const top = name => {
    const mesh = meshes.find(part => part.userData.name === name);
    return new THREE.Box3().setFromObject(mesh, true).max.y;
  };
  assert.ok(Math.abs(top("Scalloped ribbon seal") - top("Envelope body")) < 1e-5,
    "ribbon and envelope top edges align");
  for (const mesh of meshes) {
    assert.equal(mesh.material.transmission, 1, mesh.name);
    assert.ok(mesh.material.thickness > 0 && Number.isFinite(mesh.material.attenuationDistance));
  }
  // Probe the heading and four inscription grooves on the front of the plaque.
  const ray = new THREE.Raycaster();
  function depth(y) {
    ray.set(new THREE.Vector3(0, y, 1), new THREE.Vector3(0, 0, -1));
    return ray.intersectObject(plaque)[0].distance;
  }
  for (const y of [2.60, 2.18, 1.965, 1.75, 1.535]) {
    assert.ok(depth(y) - depth(y + .08) > .035, "inscription is recessed at " + y);
  }
}

const gatewayPackets = ["Packet - entering", "Packet - stretching", "Packet - cube"];

function checkGatewayPackets(meshes, THREE) {
  const bounds = gatewayPackets.map(name => {
    const packet = meshes.find(mesh => mesh.userData.name === name);
    assert.ok(packet.material.transmission > 0, name + " uses glass");
    return new THREE.Box3().setFromObject(packet);
  });
  const [lead, middle, cube] = bounds;
  const sizes = bounds.map(box => box.getSize(new THREE.Vector3()));
  for (const box of bounds) {
    const center = box.getCenter(new THREE.Vector3());
    assert.ok(Math.abs(center.y) < 1e-5 && Math.abs(center.z) < 1e-5, "packets share the aperture axis");
  }
  assert.ok(lead.min.x < 0 && lead.max.x > 0 && -lead.min.x > lead.max.x, "lead packet is past halfway through the iris");
  assert.ok(sizes[0].x < 1 && sizes[1].x < .4 && cube.max.x < 1.2, "packet train stays compact");
  assert.ok(lead.max.x < middle.min.x && middle.max.x < cube.min.x, "packets have visible gaps");
  assert.ok(sizes[0].x > sizes[1].x && sizes[1].x > sizes[2].x);
  assert.ok(Math.abs(sizes[2].x - sizes[2].y) < 1e-5 && Math.abs(sizes[2].x - sizes[2].z) < 1e-5);
}

function checkShieldProfile(meshes, THREE) {
  // The shield and its lettering wrap around the same vertical cylinder.
  const radiusRange = mesh => {
    const points = mesh.geometry.getAttribute("position");
    let inner = Infinity, outer = 0;
    for (let i = 0; i < points.count; i++) {
      const radius = Math.hypot(points.getX(i), points.getZ(i) + .68);
      inner = Math.min(inner, radius);
      outer = Math.max(outer, radius);
    }
    return {inner, outer};
  };
  const shield = meshes.find(mesh => mesh.userData.name === "Ruby shield");
  assert.ok(shield.material.transmission >= .95, "the shield transmits the scene behind it");
  assert.equal(shield.material.metalness, 0);
  assert.ok(shield.material.thickness > 0 && Number.isFinite(shield.material.attenuationDistance));
  assert.ok(shield.material.attenuationColor.r > shield.material.attenuationColor.g);
  const shieldRadius = radiusRange(shield);
  const ray = new THREE.Raycaster();
  function hitsShield(x, y) {
    ray.set(new THREE.Vector3(x, y, 2), new THREE.Vector3(0, 0, -1));
    return ray.intersectObject(shield).length > 0;
  }
  assert.ok(hitsShield(0, .6) && hitsShield(0, -.6), "glass fills the shield above and below TLS");
  assert.ok(!hitsShield(0, .04), "the lettering opening passes through the glass");
  for (const mesh of meshes.filter(mesh => mesh.userData.name.startsWith("Ivory "))) {
    assert.ok(radiusRange(mesh).inner > shieldRadius.outer, "each letter follows the outside of the shield");
  }
}

function checkVaultDoor(meshes, THREE) {
  const mesh = name => meshes.find(part => part.userData.name === name);
  const door = mesh("Open ruby door"), hinge = door.parent;
  assert.equal(hinge.userData.name, "Door hinge pivot");
  assert.ok(door.material.transmission > 0 && mesh("Secret cartridge").material.transmission > 0);
  // Close the hinge in glTF space. The vault opens toward +Z.
  hinge.quaternion.identity();
  hinge.updateWorldMatrix(true, true);
  const front = new THREE.Box3().setFromObject(door, true).max.z;
  const wheel = meshes.filter(part => part.userData.name?.startsWith("Wheel "));
  assert.equal(wheel.length, 6);
  for (const part of wheel) {
    assert.equal(part.parent, hinge);
    assert.ok(new THREE.Box3().setFromObject(part, true).min.z > front, part.name);
  }
}

const customLandmarks = {
  "cloudflare/dns.glb": ["Amber cloud", "Hexagonal post", "Left collar", "Right collar"],
  "aws/acm.glb": ["Ruby shield", "Ivory T", "Ivory L", "Ivory S"],
  "aws/api-gateway.glb": ["Aperture rim", "Iris blade 03", "Chevron 08", ...gatewayPackets],
  "aws/cloudwatch.glb": ["Ticker housing", "Left raised rim", "Right raised rim", "Glass bottom plate", "Feed slot", "Curled log strip", "Timestamp 1", "Message 1", "Timestamp 2", "Message 2", "Timestamp 3", "Message 3"],
  "aws/cognito.glb": ["Swept ruby-glass leg", "Spindle", "Fluted thumbwheel", "Profile card 01", "Profile card 34", "Profile head", "Profile shoulders"],
  "aws/dynamodb.glb": ["Key puzzle piece", "Value puzzle piece"],
  "aws/eventbridge.glb": ["Continuous fork deck", "Center slot", "Entry envelope", "Second entry envelope", "Left exit envelope", "Right exit envelope"],
  "aws/iam.glb": ["Scalloped ruby seal", "Inset ruby face", "Raised ivory check", "Left ribbon tail", "Right ribbon tail"],
  "aws/kms.glb": ["Center key", "Left key", "Right key", "Faceted collar"],
  "aws/lambda.glb": ["Main diagonal", "Left leg"],
  "aws/neptune.glb": ["Node 0", "Node 1", "Node 2"],
  "aws/rds.glb": ["Left table", "Right table", "Table link"],
  "aws/route53.glb": ["Raised 53", "Hexagonal post", "Round foot", "Right collar", "Left collar", "Right socket", "Left socket", "Right arrow", "Left arrow", "Right front inlay", "Left front inlay"],
  "aws/secretsmanager.glb": ["Open ruby door", "Raised door panel", "Wheel hub", "Wheel spoke 3", "Secret cartridge", "Upper cartridge band"],
  "aws/ses.glb": ["Engraved glass plaque", "Amber certificate frame", "Envelope flap", "Scalloped ribbon seal", "Ribbon tail left", "Ribbon tail right"],
  "aws/s3.glb": ["S3 bucket", "Stored object 1", "Stored object 2", "Stored object 3"]
};

test("the shipped renderer loads every embedded service model without external resources", async () => {
  const context = {console, TextDecoder, TextEncoder, ArrayBuffer, AbortController};
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, "vendor/topology-three.js"), "utf8"), context);
  const {GLTFLoader, THREE} = context.WizardTopologyThree;
  const directory = path.join(__dirname, "../../../awsicons/models");
  const files = fs.readdirSync(directory).flatMap(provider =>
    fs.readdirSync(path.join(directory, provider)).filter(name => name.endsWith(".glb")).map(name => provider + "/" + name));
  for (const file of Object.keys(customLandmarks)) assert.ok(files.includes(file), file);
  for (const relativePath of files) {
    const file = path.basename(relativePath);
    const bytes = fs.readFileSync(path.join(directory, relativePath));
    const gltf = await new GLTFLoader().parseAsync(bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength), "");
    const meshes = [];
    gltf.scene.traverse(object => {if (object.isMesh) meshes.push(object);});
    if (customLandmarks[relativePath]) {
      if (file === "ses.glb") checkSenderCertificate(meshes, THREE);
      if (file === "secretsmanager.glb") checkVaultDoor(meshes, THREE);
      if (file === "acm.glb") checkShieldProfile(meshes, THREE);
      if (file === "api-gateway.glb") checkGatewayPackets(meshes, THREE);
      const names = meshes.map(mesh => mesh.userData.name);
      for (const landmark of customLandmarks[relativePath]) assert.ok(names.includes(landmark), file + " missing " + landmark);
    } else {
      assert.equal(meshes.length, 7, file);
      assert.ok(meshes.some(mesh => mesh.material.isMeshPhysicalMaterial && mesh.material.transmission === .873), file);
    }
    const bounds = new THREE.Box3().setFromObject(gltf.scene);
    const size = bounds.getSize(new THREE.Vector3());
    assert.ok(size.toArray().every(extent => Number.isFinite(extent) && extent > 0), file);
  }
});
