"""Import downloaded model and render three independent inspection angles."""
import bpy, json, sys, math
from pathlib import Path
from mathutils import Vector
out = Path(__import__('os').environ.get('DUIVEN_MODEL_CANDIDATES') or sys.exit('zet DUIVEN_MODEL_CANDIDATES (map met de kandidaatmodellen)'))
model = out / (sys.argv[sys.argv.index('--')+1] if '--' in sys.argv else 'cher-ami-download.glb')
bpy.ops.object.select_all(action='SELECT'); bpy.ops.object.delete(use_global=False)
bpy.ops.import_scene.gltf(filepath=str(model))
stem=model.stem
meshes = [o for o in bpy.context.scene.objects if o.type == 'MESH']
points = [o.matrix_world @ Vector(c) for o in meshes for c in o.bound_box]
low = Vector([min(p[i] for p in points) for i in range(3)])
high = Vector([max(p[i] for p in points) for i in range(3)])
center=(low+high)/2; span=max(high-low)
report={'objects':[{'name':o.name,'vertices':len(o.data.vertices),'polygons':len(o.data.polygons),'dimensions':list(o.dimensions)} for o in meshes],
'materials':[m.name for m in bpy.data.materials], 'images':[{'name':i.name,'size':list(i.size)} for i in bpy.data.images], 'low':list(low), 'high':list(high),
'armatures':[o.name for o in bpy.context.scene.objects if o.type=='ARMATURE']}
(out/f'{stem}-blender-report.json').write_text(json.dumps(report,indent=2))
scene=bpy.context.scene; scene.render.engine='CYCLES';scene.cycles.samples=16
scene.render.resolution_x=900;scene.render.resolution_y=900;scene.render.resolution_percentage=100
scene.render.image_settings.file_format='PNG';scene.render.film_transparent=False
scene.world.color=(.45,.45,.45)
for loc,power,size in [((2,-3,4),1300,3),((-3,-1,2),750,2),((1,3,3),1200,2)]:
 bpy.ops.object.light_add(type='AREA',location=center+Vector(loc)*span)
 l=bpy.context.object;l.data.energy=power*span*span;l.data.shape='DISK';l.data.size=size*span
 l.rotation_euler=(center-l.location).to_track_quat('-Z','Y').to_euler()
bpy.ops.object.camera_add();cam=bpy.context.object;scene.camera=cam;cam.data.type='ORTHO';cam.data.ortho_scale=span*1.3
for name,vec in [('front',(1,-2,.65)),('side',(2,.5,.55)),('back',(-1.5,2,.6))]:
 cam.location=center+Vector(vec)*span;cam.rotation_euler=(center-cam.location).to_track_quat('-Z','Y').to_euler()
 scene.render.filepath=str(out/f'{stem}-{name}.png');bpy.ops.render.render(write_still=True)
bpy.ops.wm.save_as_mainfile(filepath=str(out/f'{stem}-inspection.blend'))
print(json.dumps(report))
