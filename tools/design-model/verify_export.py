"""Re-import the actual GLB into a fresh scene, verify rigs, render proof.

Run after build_pigeon.py using Blender --background --python this_file.
This validates portability to Blender's glTF importer, not browser playback.
"""
import bpy, os, sys

out = os.environ.get('DUIVEN_MODEL_OUTPUT') or sys.exit('zet DUIVEN_MODEL_OUTPUT (map met het geexporteerde model)')
bpy.ops.wm.open_mainfile(filepath=os.path.join(out, 'stadsduif.blend'))
for ob in list(bpy.data.objects):
    if ob.type not in {'CAMERA', 'LIGHT'}:
        bpy.data.objects.remove(ob, do_unlink=True)
bpy.ops.import_scene.gltf(filepath=os.path.join(out, 'stadsduif.glb'))
required = ['PigeonRoot', 'NeckRig', 'WingLeft', 'WingRight', 'TailRig', 'BodyRig']
for name in required:
    assert bpy.data.objects.get(name), 'Missing rig group: ' + name
meshes = [o for o in bpy.context.scene.objects if o.type == 'MESH']
assert meshes, 'No meshes imported'
assert all(len(o.data.materials) for o in meshes), 'Missing materials'
scene=bpy.context.scene
scene.render.filepath=os.path.join(out, 'stadsduif-glb-check.png')
bpy.ops.render.render(write_still=True)
print('GLB_REIMPORT_VERIFIED', len(meshes), 'meshes; six required groups present')
