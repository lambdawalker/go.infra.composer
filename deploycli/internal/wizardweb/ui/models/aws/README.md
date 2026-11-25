# AWS glass cube models

These thirteen GLB files are unchanged copies from the published
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
| `acm.glb` | `security-identity-compliance-certificate-manager-cube.glb` | 109108 | `2edb67f7eadc540b6124d3b288fa251e35b948e8b3c183838c8241165cf4a55b` |
| `api-gateway.glb` | `app-integration-api-gateway-cube.glb` | 108532 | `f7f8506b7852e984d415ef8b53260d783bac3f72647de89612a7aca5f22f906e` |
| `cloudwatch.glb` | `management-governance-cloudwatch-cube.glb` | 397976 | `87c3250141d9afc1444c7283c938799ad5dc073d3c5f36f088d5dbee2e73883e` |
| `cognito.glb` | `security-identity-compliance-cognito-cube.glb` | 284504 | `5db6640394e2cd786ebf23eba76fc5139ad53cfe71a3188a623cd086bd5bfc85` |
| `dynamodb.glb` | `database-dynamodb-cube.glb` | 322232 | `a07577e8b8a5b09f06337b3b1c78978ab6125f1dacb858fcdf724c18edcd0546` |
| `eventbridge.glb` | `app-integration-eventbridge-cube.glb` | 269240 | `d709b789e6e84c000c74273c04574e61ff3efb0529559926074ff5d9263efa13` |
| `iam.glb` | `security-identity-compliance-identity-and-access-management-cube.glb` | 143668 | `795f1c8012ce45784e7ec965fdcd6a33bc6fa0fc817e172f5b6c6de69a7e9162` |
| `kms.glb` | `security-identity-compliance-key-management-service-cube.glb` | 267508 | `f4aef5be2d17323c91f9f4e837babef0583c259e38415c9432e9d884222aae7d` |
| `lambda.glb` | `compute-lambda-cube.glb` | 89204 | `1a79abf21c689abe0c16bd9da93d0883f8a53b64028fb9f8c8f7832f7b609406` |
| `route53.glb` | `networking-content-delivery-route-53-cube.glb` | 483512 | `170b915f744f1610d7aad64ef9464763216635e8aa9a4e22a332f085c3e74cec` |
| `s3.glb` | `storage-simple-storage-service-cube.glb` | 224052 | `33d2adeb5713c26c9b8fd86508d3751a08c82bc0aa296d3a5d4ec25e791edd81` |
| `secretsmanager.glb` | `security-identity-compliance-secrets-manager-cube.glb` | 271832 | `acd372a8f82584ee078d6096ae5119455841dd72f68cd61abfc95c09b246d496` |
| `ses.glb` | `business-applications-simple-email-service-cube.glb` | 306104 | `24a6790452ffafe219b7940cf9c09b966e5ec8b1d1984ed961e34a0b840ddaa3` |
