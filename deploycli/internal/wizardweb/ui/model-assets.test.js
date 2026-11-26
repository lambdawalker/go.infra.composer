"use strict";
const assert = require("node:assert/strict");
const test = require("node:test");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

test("bundled AWS cubes contain their geometry and preserve the catalog materials", () => {
  const services = ["acm", "cloudwatch", "cognito", "eventbridge",
    "route53", "secretsmanager", "ses"];
  for (const service of services) {
    const bytes = fs.readFileSync(path.join(__dirname, "../../../awsicons/models/aws", service + ".glb"));
    assert.equal(bytes.toString("ascii", 0, 4), "glTF", service);
    assert.equal(bytes.readUInt32LE(4), 2, service);
    assert.equal(bytes.readUInt32LE(8), bytes.length, service);
    const jsonLength = bytes.readUInt32LE(12);
    assert.equal(bytes.toString("ascii", 16, 20), "JSON", service);
    const model = JSON.parse(bytes.toString("utf8", 20, 20 + jsonLength));
    assert.equal(model.nodes.length, 7, service + " has a cube and six symbol faces");
    assert.ok(model.buffers.every(buffer => !buffer.uri), service + " is self-contained");
    assert.ok(!model.images?.length, service + " has geometric symbols, not image planes");
    const glass = model.materials[0];
    assert.equal(glass.extensions.KHR_materials_transmission.transmissionFactor, .873, service);
    assert.equal(glass.extensions.KHR_materials_volume.thicknessFactor, 2, service);
    assert.deepEqual(model.accessors[0].min, [-1,-1,-1], service);
    assert.deepEqual(model.accessors[0].max, [1,1,1], service);
    assert.ok(Math.abs(model.accessors[2].min[2] + .115) < 1e-6, service);
    assert.ok(Math.abs(model.accessors[2].max[2] - .066) < 1e-6, service);
  }
});

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
  assert.ok(Math.abs(lead.min.x + lead.max.x) < 1e-5, "lead packet is halfway through the iris");
  assert.ok(lead.max.x < middle.min.x && middle.max.x < cube.min.x, "packets have visible gaps");
  assert.ok(sizes[0].x > sizes[1].x && sizes[1].x > sizes[2].x);
  assert.ok(Math.abs(sizes[2].x - sizes[2].y) < 1e-5 && Math.abs(sizes[2].x - sizes[2].z) < 1e-5);
}

const customLandmarks = {
  "api-gateway.glb": ["Aperture rim", "Iris blade 03", "Chevron 08", ...gatewayPackets],
  "dynamodb.glb": ["Key puzzle piece", "Value puzzle piece"],
  "iam.glb": ["Scalloped ruby seal", "Inset ruby face", "Raised ivory check", "Left ribbon tail", "Right ribbon tail"],
  "kms.glb": ["Center key", "Left key", "Right key", "Faceted collar"],
  "lambda.glb": ["Main diagonal", "Left leg"],
  "neptune.glb": ["Node 0", "Node 1", "Node 2"],
  "rds.glb": ["Left table", "Right table", "Table link"],
  "s3.glb": ["S3 bucket", "Stored object 1", "Stored object 2", "Stored object 3"]
};

test("the shipped renderer loads every embedded service model without external resources", async () => {
  const context = {console, TextDecoder, TextEncoder, ArrayBuffer, AbortController};
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, "vendor/topology-three.js"), "utf8"), context);
  const {GLTFLoader, THREE} = context.WizardTopologyThree;
  const directory = path.join(__dirname, "../../../awsicons/models/aws");
  const files = fs.readdirSync(directory).filter(name => name.endsWith(".glb"));
  for (const file of Object.keys(customLandmarks)) assert.ok(files.includes(file), file);
  for (const file of files) {
    const bytes = fs.readFileSync(path.join(directory, file));
    const gltf = await new GLTFLoader().parseAsync(bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength), "");
    const meshes = [];
    gltf.scene.traverse(object => {if (object.isMesh) meshes.push(object);});
    if (customLandmarks[file]) {
      if (file === "api-gateway.glb") checkGatewayPackets(meshes, THREE);
      const names = meshes.map(mesh => mesh.userData.name);
      for (const landmark of customLandmarks[file]) assert.ok(names.includes(landmark), file + " missing " + landmark);
    } else {
      assert.equal(meshes.length, 7, file);
      assert.ok(meshes.some(mesh => mesh.material.isMeshPhysicalMaterial && mesh.material.transmission === .873), file);
    }
    const bounds = new THREE.Box3().setFromObject(gltf.scene);
    const size = bounds.getSize(new THREE.Vector3());
    assert.ok(size.toArray().every(extent => Number.isFinite(extent) && extent > 0), file);
  }
});
