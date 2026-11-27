# Topology runtime

This build bundles Three.js, its glTF loader, and the procedural room lighting
used by the topology's AWS service models. The wizard serves the checked-in bundle
locally; running the deployment CLI does not require Node or a CDN.

To rebuild after changing dependencies or exports, run `npm ci` and `npm run build`
in this directory. Commit the lockfile, generated `ui/vendor/topology-three.js`,
and the copied Three.js MIT license together. The model provenance is recorded
in the [shared AWS model catalog](../../../awsicons/models/aws/README.md).

For nested glass with overlapping depth ranges, set the integer glTF node extra
`transmissionLayer` on enclosing meshes. The compositor processes smaller
values first within the same artifact. Ordinary parts default to zero.
