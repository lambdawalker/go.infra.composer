# AWS service models

The wizard embeds these service-specific models from the custom icon collection.

Glass bodies use the certificate palette: light surface tints, saturated volume absorption, a polished clearcoat and a refractive index of 1.48. Hardware, markings and selected enamel details retain their own finishes.

For engraved certificate icons, use one shared accent glass material for the frame and every add-on, including the identifying symbol, ribbon seal and tails. Use a distinct accent color for each certificate type and neutral glass for the engraved plaque.

| Local file | Design | SHA-256 |
| --- | --- | --- |
| `ses.glb` | Engraved glass sender certificate with a plum frame, envelope and ribbon | `1689072dd4ecb349fba9046e13d7fc50e95ada0d266ad6b2e306e90b7aedc3a6` |
| `secret-version.glb` | Rolled champagne glass manuscript with a woven teal binding and ruby wax seal | `b68da9e1f51354a09c67b6548dfe621090ba2a390913e55f51cc2710a09993dc` |
| `secretsmanager.glb` | Ruby glass vault with an exterior three-spoke handle and a frosted cyan secret cartridge | `57e031a3598ef8065aa5a7f9761a68b2fcfcf398793ad3cda8575ca807720af2` |
| `eventbridge.glb` | Raspberry-glass fork with two queued entry envelopes and an amber envelope midway along each of two exits | `6d50fa1b95e371f9b4eff349f85c01d52cc30aa326370e875ab03052860a1156` |
| `cognito.glb` | Classic Rotary user directory with 34 frosted cards, a raised ruby profile and curved glass supports | `9e0a61b6565368cbe8a59f3d41c5306cd7080fd8aa551ee2163fa427f3be6b27` |
| `cloudwatch.glb` | Hollow raspberry-glass ticker with raised side rims, a framed feed slot and a curled ivory log strip | `4e4183c92c44cfc395d7395b5813f9921b6b8c4e74d043c26c22adad5aaa1c56` |
| `acm.glb` | Engraved glass TLS certificate with a sapphire frame, raised TLS letters and ribbon seal | `1ad620958e9a3a65958274ca39eb0236a62ef83c0805a3003b5bb0a8abb43152` |
| `iam-role.glb` | IAM role plaque with ruby frame, person silhouette and matching ribbon | `d9a87301ea8e164c89e92948e58b8b4e4947aacecf4e65d00522b868f192f57a` |
| `acm-validation.glb` | ACM certificate validation plaque with emerald frame, ACM and matching ribbon | `8a9454a3ac81a57e2fb8060d4dbb2da366fd297db96eeda13bc02f0d9c3bac90` |
| `api-gateway-jwt.glb` | API Gateway JWT authorizer plaque with teal frame, JWT and matching ribbon | `263d3e2d7fc35566a3dcc642b9ad96b821b5f2e015aa6737dfb2d6b33c2ebdb0` |
| `acm-shield.glb` | Alternate topology model. Transmissive ruby glass shield with a beveled opening for ivory TLS letters on its convex face | `6a00fe0b27967c7d10cef4a86be671ecbe4b4759370dea7b4b86714b052066b1` |
| `api-gateway-integration.glb` | Rounded violet glass clevis and orange glass tongue joined by a brushed silver pin | `e8501d198eac4b15959111eca1ab3a5e0df9573b3e3099e6000c4340a12e569e` |
| `api-gateway-route.glb` | Violet glass sextant with a round port cap, diagonal telescope mounted in front of its frosted index arm, and cyan packet slider | `40a9c4a3979b5c39dd5753df2b4026fe2be5923916eff5d9e30364cdd0a87895` |
| `api-gateway.glb` | Violet aperture with three compact cyan glass packets entering the iris | `9b1915ce7a36bf42ec624e5596b5f9e6ae67872ce327ea1b2f312a6bd160f830` |
| `route53.glb` | Violet glass signpost topped with a raised 53 plate | `99fc08f19b3185b1b6e57e84bcef8d8156498fe75d87d61502c81380abdd3b44` |
| `../cloudflare/dns.glb` | Matching DNS signpost topped with an amber cloud | `01eb9c60489c38f3735ae07f177d566cf56c1b883ae7492bacd9563bb24146a5` |
| `lambda-permission.glb` | Engraved glass certificate with a mandarin-orange frame, Lambda and ribbon | `5dc038e6f7fdd2e9bc7798e522566c4482edeff10a0378a277aa48fbf5c5fb9e` |
| `lambda.glb` | Split orange Lambda | `e5ad016aeb5e8667d15572dedd6710cb9eb3ef3c856a4f3bbbbd77d42baac13f` |
| `s3.glb` | Green bucket with stored objects | `4245276248537e897fb5f28de1f86f1943ccffc17fc5d2ffe3933c99c9e539cb` |
| `iam.glb` | Scalloped ruby permission seal with an ivory check mark and two notched ribbons | `653084edec32bb6ed0888b5500113ac0fc2359ab79e4bc6be1a383799aece8dc` |
| `kms.glb` | Three red keys with a collar | `e8bdfb3456319afc1deedb203bf1089ba8ba3c286455427299a7b54373e77c4f` |
| `dynamodb.glb` | Database with paired puzzle pieces | `7e21c14745790f4b0c77fcb27801346bab90eaa82787907133ec779a608e610e` |
| `rds.glb` | Database with linked tables | `8351ec50a9c0498bc6cf4ee2fe3fbf0cd55555f3ea6fe11293c97e5f414c1a48` |
| `neptune.glb` | Database with a three-node graph | `4c7dfd2367f48a35c47377256157dcb8665c62c91b326942768fca99fa7368af` |

All meshes and materials are embedded in the GLBs. The manuscript includes embedded textures for its woven binding. The models require no external buffers or textures, decompression extensions, or network resources. A glTF renderer
with `KHR_materials_transmission` and `KHR_materials_volume` support is needed to
preserve the glass finish.
