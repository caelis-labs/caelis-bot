# Character studio

Both shipped characters are available in **Settings → Appearance → Character and costume**.
The [built-in manifest](../resources/character-pack.json) pins their finished assets.
Application builds use these files directly. Blender 5.2.1 is used for authoring.

| Asset | Editable source and dependencies |
| --- | --- |
| Caelis, sage dress | [Shipped model](caelis/shipped-sage-dress.blend), [interaction poses](caelis/interaction-poses.json), [source notice](../frontend/public/models/caelis-SOURCES.md). The [shared sculpt](caelis/sage-dress.blend) also supplies the Whale Girl generator. |
| Whale Girl, maid outfit (1.26.0) | [Shipped model](whale-girl/whale-girl.blend), [generator](whale-girl/model.py), [body clay](whale-girl/studies/body-clay-v1.blend), [reusable 1.18 face](whale-girl/reference/face-baseline-1.18.blend), [preview](whale-girl/preview.png), [attribution and license](whale-girl/LICENSE.md). |
| Reusable skeletons | [Caelis 61-bone FK](rigs/caelis-humanoid-fk.blend), [22-bone rest](rigs/humanoid-rest.blend), [25- and 198-bone rest hierarchies](rigs/q-rest.blend). These libraries contain armatures only. The Q hierarchies reflect Blender Rigify Basic Human proportions. An earlier study used Julien Kaspar’s CC0 Body Female – Stylized mesh from Blender Human Base Meshes 1.4.1. |
| Paper plane and branding | [Paper plane source](props/paper-plane.blend), [transparent avatar](brand/avatar-transparent.png) and [icon master](brand/app-icon-1024.png). Run `bash art/tools/prepare-brand-icons.sh` on macOS for the icon set. |

## Re-export

Run from the repository root. The shared sculpt and shipped Caelis scene are both
inputs to the Whale Girl generator. Edit the shipped Blender scenes directly for
manual modeling. Running the Whale Girl generator replaces its editable scene.

```sh
mkdir -p .cache/art
blender -b --disable-autoexec art/caelis/shipped-sage-dress.blend --python-exit-code 1 --python art/caelis/check-body.py
blender -b --disable-autoexec art/caelis/shipped-sage-dress.blend --python-exit-code 1 --python art/tools/export_glb.py -- .cache/art/base.glb --caelis-base
blender -b --disable-autoexec art/caelis/shipped-sage-dress.blend --python-exit-code 1 --python art/tools/export_face.py -- .cache/art/base.glb .cache/art/face.glb
node art/tools/finish-glb.mjs .cache/art/face.glb .cache/art/caelis.glb

blender -b --disable-autoexec art/whale-girl/whale-girl.blend --python-exit-code 1 --python art/whale-girl/check-body.py
blender -b --disable-autoexec art/whale-girl/whale-girl.blend --python-exit-code 1 --python art/tools/export_glb.py -- .cache/art/whale-girl.glb
node art/whale-girl/viewer/build.mjs .cache/art/whale-girl.html
```

The offline viewer embeds the shipped Whale Girl GLB. To change the costume,
fit clothing to the same armature in its own collection with a `caelisOutfit`
property. Follow the maid pieces’ `deform_group` and `caelisLayer` conventions,
enable the desired costume and export the whole figure as a variant.
See the [content-pack contract](../docs/content-packs.md) for distribution and verification.
