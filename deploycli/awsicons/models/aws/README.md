# AWS service models

The wizard embeds these service-specific models from the custom icon collection.

| Local file | Design | SHA-256 |
| --- | --- | --- |
| `ses.glb` | Engraved glass sender certificate with an amber frame and matching plum envelope and ribbon | `910f654bba8e027abfc97501472831573c523904f9224c5e3e39b00486361ffe` |
| `secretsmanager.glb` | Ruby glass vault with an exterior three-spoke handle and a frosted cyan secret cartridge | `f457ab02fbc3faa3a7b9b157cf7cd104f3a0a4e40734954055baa118efe502d3` |
| `eventbridge.glb` | Raspberry-glass fork with two queued entry envelopes and an amber envelope midway along each of two exits | `d6d874fc7bee48c8427b17a41f5af5144b5c703d7ac41c324fc0212253540d43` |
| `cognito.glb` | Classic Rotary user directory with 34 frosted cards, a raised ruby profile and curved glass supports | `147c3c9c0632780ca2863e17f1d22780a26784cec8a52e9e6b00c4b506784f2f` |
| `cloudwatch.glb` | Hollow raspberry-glass ticker with raised side rims, a framed feed slot and a curled ivory log strip | `ab5956230e9d6105a4b272d8b278b69825231b2b357cc28c0ea65c08125f18f5` |
| `acm.glb` | Engraved glass TLS certificate with raised teal letters and a sapphire ribbon seal | `5b1af3cd6536f4c45310c1c7ca7cccae7e98fd2294bad7aeb74b94e0dca466b3` |
| `acm-shield.glb` | Alternate topology model. Transmissive ruby glass shield with a beveled opening for ivory TLS letters on its convex face | `6a00fe0b27967c7d10cef4a86be671ecbe4b4759370dea7b4b86714b052066b1` |
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

All meshes and materials are embedded in the GLBs. They require no textures,
external buffers, decompression extensions, or network resources. A glTF renderer
with `KHR_materials_transmission` and `KHR_materials_volume` support is needed to
preserve the glass finish.
