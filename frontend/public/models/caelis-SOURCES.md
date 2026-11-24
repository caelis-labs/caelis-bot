# Character distribution notes

Current character: Caelis, sage dress. Model, avatar and brand icons use the
Caelis Character Asset License (`ASSET-LICENSE.md`), separately from the app code.
The stick fallback and generic paper-plane mesh are Apache-2.0.

The Caelis costume was made for this project from a user-provided character sheet.
The refined face, pink hair, body and rig retain earlier Xiaoai-derived design work;
the avatar also retains that design lineage. Changing the outfit does not erase
these origins or establish independent clearance of underlying reference art.
The current sage-dress source fits the project-authored neutral body study to
the Caelis shoulder, waist and hip proportions. It joins the torso to the original
neck, arms and legs and retains the Caelis skeleton and finger rig. The editable
model and shared skeleton are maintained under art/ in this repository.

No downloaded third-party finished model, texture or font is embedded in these GLBs.
Only finished runtime meshes, materials, rigs, morphs, animation and required pose
metadata are distributed. Application bundles use the finished assets listed in the character pack.
The separate asset license grants only rights controlled by the maintainer.

The `portraits/caelis-sage-v2` finished bust atlas set is rendered from the fitted
sage-dress GLB identified by `portrait.sourceSHA256` in the official pack manifest.
Eight local expression/pose recipes produce 120 frames at 20fps per clip; all use
the same neutral loop boundary. Static alpha WebP atlases and the neutral PNG
retain the model's design lineage and Caelis Character Asset License.
