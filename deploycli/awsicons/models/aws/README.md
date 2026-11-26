# AWS service models

The wizard embeds these service-specific models from the custom icon collection.

| Local file | Design | SHA-256 |
| --- | --- | --- |
| `cloudwatch.glb` | Hollow raspberry-glass ticker with raised side rims, a framed feed slot and a curled ivory log strip | `ab5956230e9d6105a4b272d8b278b69825231b2b357cc28c0ea65c08125f18f5` |
| `acm.glb` | Transmissive ruby glass shield with a beveled opening for ivory TLS letters on its convex face | `6a00fe0b27967c7d10cef4a86be671ecbe4b4759370dea7b4b86714b052066b1` |
| `api-gateway.glb` | Violet aperture with three compact cyan glass packets entering the iris | `931ad351625dc50cd70eb1ab8e668f9f6766b717184a0400d3e881dc26310bc9` |
| `route53.glb` | Violet glass signpost topped with a raised 53 plate | `88dd26ae9ff936196a7f18b14b89725eb5fa52a443e46269582d7e82a94eaefe` |
| `../cloudflare/dns.glb` | Matching DNS signpost topped with an amber cloud | `adb79b5425b190a406a67fc42a68291de61b837575a7682a64ee80b08918b74c` |
| `lambda.glb` | Split orange Lambda | `f09f6fff1490e49f2a32b220ab49d6235841ecf2797fc12ce64313be4aa5096d` |
| `s3.glb` | Green bucket with stored objects | `1fdd247b9bdf49d9ae6142884247313411f3c84687b9018488632a33d745b650` |
| `iam.glb` | Scalloped ruby permission seal with an ivory check mark and two notched ribbons | `a6c9f6731b90868eca92b39084af2f11c7135266c64a324e1b313a0aa7d412c5` |
| `kms.glb` | Three red keys with a collar | `477478d6c2a6947153da352fe90c6da17bb60e1d7f4eaca2ef569b4e127c185f` |
| `dynamodb.glb` | Database with paired puzzle pieces | `38c7d7e48a2da66c235afc74b43f1dfc99be8db7ab9db460130e0ee36b5e9c5a` |
| `rds.glb` | Database with linked tables | `6662da48bc2a40a372087535155acf1612b8fad95efa61fba11b7dab23b66f82` |
| `neptune.glb` | Database with a three-node graph | `42fa3a8c688f2098da712e736833f9f87db39b72aa84104e05eb330cc22220ae` |

## Glass cube catalog

These four GLB files are unchanged copies from the published
[AWS Glass Cubes catalog](https://aws-glass-cubes.lemony-amber-5141.chatgpt.site),
version 3, source commit `64e50dcef5cef7eeb619d216c4d24f310acb84e6`.
The source archive is `dist/aws-glass-cubes.zip`; only the filenames were shortened
for the wizard's service mapping. No runtime request goes to the catalog.

The catalog defaults are preserved: glass opacity **55%**, white stroke width
**85%**, white extrusion **120%**, and white inward depth **11.5%**. In glTF these
models use transmission `0.873`, volume thickness `2`, attenuation distance `3.6`,
and glyph depth from `-0.115` to `0.066` relative to each cube face. Each model
contains one rounded glass cube spanning `[-1, 1]` on every axis and the same
extruded white service glyph on all six faces.

The original AWS service artwork is owned by Amazon Web Services, Inc. The source
artwork comes from [AWS Icons](https://aws-icons.com/), which credits the
[AWS Architecture Icons](https://aws.amazon.com/architecture/icons/) package.
Preserve the AWS artwork attribution when reusing these models.

All meshes and materials are embedded in the GLBs. They require no textures,
external buffers, decompression extensions, or network resources. A glTF renderer
with `KHR_materials_transmission` and `KHR_materials_volume` support is needed to
preserve the glass finish.

| Local file | Archive entry | Bytes | SHA-256 |
| --- | --- | ---: | --- |
| `cognito.glb` | `security-identity-compliance-cognito-cube.glb` | 284504 | `5db6640394e2cd786ebf23eba76fc5139ad53cfe71a3188a623cd086bd5bfc85` |
| `eventbridge.glb` | `app-integration-eventbridge-cube.glb` | 269240 | `d709b789e6e84c000c74273c04574e61ff3efb0529559926074ff5d9263efa13` |
| `secretsmanager.glb` | `security-identity-compliance-secrets-manager-cube.glb` | 271832 | `acd372a8f82584ee078d6096ae5119455841dd72f68cd61abfc95c09b246d496` |
| `ses.glb` | `business-applications-simple-email-service-cube.glb` | 306104 | `24a6790452ffafe219b7940cf9c09b966e5ec8b1d1984ed961e34a0b840ddaa3` |
