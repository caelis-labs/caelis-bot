import { observeCharacterCanvas } from '../../../frontend/src/character/canvas-resolution';
import { AnimationMixer, Group, LoopOnce, LoopRepeat, MathUtils, OrthographicCamera, Scene, WebGLRenderer } from 'three';
import { GLTFLoader } from 'three/addons/loaders/GLTFLoader.js';
import { OrbitControls } from 'three/addons/controls/OrbitControls.js';
import { characterProfile } from '../../../frontend/src/character/profile.ts';

const byId = id => document.getElementById(id);
const canvas = byId('stage');
const status = byId('status');
const license = byId('license');
byId('credits').onclick = () => license.showModal();
const controlsUI = document.querySelectorAll('main button, main select');
controlsUI.forEach(control => { control.disabled = true; });

async function modelBytes() {
  const payload = byId('model-bytes');
  const decoded = Uint8Array.from(atob(payload.textContent.trim()), c => c.charCodeAt(0));
  payload.remove();
  const stream = new Blob([decoded]).stream().pipeThrough(new DecompressionStream('gzip'));
  return new Response(stream).arrayBuffer();
}

async function start() {
  const renderer = new WebGLRenderer({ canvas, alpha: true, antialias: true });
  const gltf = await new GLTFLoader().parseAsync(await modelBytes(), '');
  const scene = new Scene();
  const root = new Group();
  root.userData.desktopPetProfile = 'reference-v1';
  root.add(gltf.scene);
  scene.add(root);
  characterProfile(renderer, scene, root);

  const camera = new OrthographicCamera(-1, 1, 1, -1, .1, 30);
  const orbit = new OrbitControls(camera, canvas);
  orbit.enablePan = false;
  orbit.minZoom = .65;
  orbit.maxZoom = 3.5;
  orbit.minPolarAngle = Math.PI * .12;
  orbit.maxPolarAngle = Math.PI * .84;
  orbit.autoRotateSpeed = 1.55;
  const mixer = new AnimationMixer(gltf.scene);
  const clips = new Map(gltf.animations.map(clip => [clip.name, clip]));
  // The shared ink mesh also contains the stocking and shoe silhouettes.
  const garmentLayers = new Set(['maid_dress', 'maid_apron']);
  const parts = [];
  const jawTargets = [];
  gltf.scene.traverse(part => {
    if (!part.isMesh) return;
    const index = part.morphTargetDictionary?.MouthClose;
    if (index !== undefined) jawTargets.push({weights:part.morphTargetInfluences,index});
    let layer = part;
    while (layer.parent && !layer.userData.caelisLayer && !garmentLayers.has(layer.name)) {
      layer = layer.parent;
    }
    parts.push({ part, clothing: garmentLayers.has(layer.userData.caelisLayer ?? layer.name) });
  });

  let outfit = false;
  let playing = !matchMedia('(prefers-reduced-motion: reduce)').matches;
  let finished = false;
  let active;
  let needsPaint = true;
  const mouth = byId('mouth');
  byId('mouth-control').hidden = jawTargets.length === 0;
  mouth.onchange = () => { needsPaint = true; };

  function sizeCamera() {
    const width = canvas.clientWidth, height = canvas.clientHeight;
    if (!width || !height) return;
    const aspect = width / height;
    const span = outfit ? Math.max(1.08, 1.13/aspect) : Math.max(2.10, 1.84/aspect);
    camera.top = span/2;
    camera.bottom = -camera.top;
    camera.right = camera.top*aspect;
    camera.left = -camera.right;
    camera.updateProjectionMatrix();
    needsPaint = true;
  }

  function view(yaw, pitch = Math.PI/2-.017) {
    const a = MathUtils.degToRad(yaw);
    camera.position.set(
      orbit.target.x + 5*Math.sin(pitch)*Math.sin(a),
      orbit.target.y + 5*Math.cos(pitch),
      5*Math.sin(pitch)*Math.cos(a),
    );
    orbit.update();
    needsPaint = true;
  }

  function displayAngle() {
    const degrees = MathUtils.euclideanModulo(MathUtils.radToDeg(orbit.getAzimuthalAngle()),360);
    const level = Math.abs(orbit.getPolarAngle()-(Math.PI/2-.017)) < .09;
    let label = '自由视角';
    for (const button of byId('angles').querySelectorAll('button')) {
      const offset = Math.abs(MathUtils.euclideanModulo(degrees-Number(button.dataset.yaw)+180,360)-180);
      const selected = level && offset < 2.5;
      button.setAttribute('aria-pressed', String(selected));
      if (selected) label = button.textContent;
    }
    byId('angle-label').textContent = label;
    byId('scale').value = Math.round(camera.zoom*100)+'%';
    needsPaint = true;
  }
  orbit.addEventListener('change', displayAngle);

  function spin(enabled) {
    orbit.autoRotate = enabled;
    byId('spin').setAttribute('aria-checked', String(enabled));
  }
  orbit.addEventListener('start', () => spin(false));
  byId('spin').onclick = () => spin(!orbit.autoRotate);
  for (const button of byId('angles').querySelectorAll('button')) {
    button.onclick = () => { spin(false); view(Number(button.dataset.yaw)); };
  }

  function setZoom(value) {
    camera.zoom = MathUtils.clamp(value, orbit.minZoom, orbit.maxZoom);
    camera.updateProjectionMatrix();
    displayAngle();
  }
  byId('minus').onclick = () => setZoom(camera.zoom/1.18);
  byId('plus').onclick = () => setZoom(camera.zoom*1.18);
  byId('reset').onclick = () => { spin(false); setZoom(1); view(0); };
  canvas.addEventListener('keydown', event => {
    const yaw = MathUtils.radToDeg(orbit.getAzimuthalAngle()), pitch = orbit.getPolarAngle();
    switch (event.key) {
      case 'ArrowLeft': view(yaw-15,pitch); break;
      case 'ArrowRight': view(yaw+15,pitch); break;
      case 'ArrowUp': view(yaw, Math.max(orbit.minPolarAngle,pitch-.15)); break;
      case 'ArrowDown': view(yaw, Math.min(orbit.maxPolarAngle,pitch+.15)); break;
      case '+': case '=': setZoom(camera.zoom*1.18); break;
      case '-': setZoom(camera.zoom/1.18); break;
      default: return;
    }
    spin(false);
    event.preventDefault();
  });

  function displayLayer(garmentOnly) {
    const yaw = MathUtils.radToDeg(orbit.getAzimuthalAngle());
    const pitch = orbit.getPolarAngle();
    outfit = garmentOnly;
    for (const entry of parts) entry.part.visible = !outfit || entry.clothing;
    for (const button of byId('layers').querySelectorAll('button')) {
      button.setAttribute('aria-pressed', String((button.dataset.layer==='outfit')===outfit));
    }
    orbit.target.set(outfit ? 0 : .08, outfit ? .64 : .87, 0);
    sizeCamera();
    setZoom(1);
    view(yaw, pitch);
    renderer.shadowMap.needsUpdate = true;
  }
  for (const button of byId('layers').querySelectorAll('button')) {
    button.onclick = () => displayLayer(button.dataset.layer==='outfit');
  }

  function playbackLabel() {
    byId('play').textContent = finished ? '重播' : playing ? '暂停' : '播放';
    byId('play').setAttribute('aria-pressed', String(!playing && !finished));
  }
  function action(name) {
    mixer.stopAllAction();
    active = mixer.clipAction(clips.get(name)).reset();
    const loop = name==='idle' || name==='working';
    active.setLoop(loop ? LoopRepeat : LoopOnce, loop ? Infinity : 1);
    active.clampWhenFinished = true;
    active.play();
    mixer.update(0);
    finished = false;
    playbackLabel();
    renderer.shadowMap.needsUpdate = true;
    needsPaint = true;
  }
  mixer.addEventListener('finished', () => {
    finished = true;
    playing = false;
    playbackLabel();
  });
  byId('motion').onchange = event => { playing = true; action(event.target.value); };
  byId('play').onclick = () => {
    playing = !playing;
    if (finished) action(byId('motion').value);
    playbackLabel();
  };

  orbit.target.set(.08,.87,0);
  view(0);
  sizeCamera();
  observeCharacterCanvas(renderer, sizeCamera);
  action('idle');
  let lastFrame;
  function draw(now) {
    const dt = Math.min(Math.max(0,(now-(lastFrame ?? now))/1000),.08);
    lastFrame = now;
    if (playing) {
      mixer.update(dt);
      renderer.shadowMap.needsUpdate = true;
      needsPaint = true;
    }
    orbit.update(dt);
    if (needsPaint) {
      // Override only the rendered frame so resuming the clip restores even a paused pose.
      const manual = !outfit && mouth.value !== 'auto';
      if (manual) for (const jaw of jawTargets) {
        jaw.animated = jaw.weights[jaw.index];
        jaw.weights[jaw.index] = Number(mouth.value);
      }
      renderer.render(scene,camera);
      if (manual) for (const jaw of jawTargets) jaw.weights[jaw.index] = jaw.animated;
      needsPaint = false;
    }
  }
  draw(performance.now());
  renderer.setAnimationLoop(draw);
  document.addEventListener('visibilitychange', () => {
    lastFrame = undefined;
    renderer.setAnimationLoop(document.hidden ? null : draw);
  });
  controlsUI.forEach(control => { control.disabled = false; });
  status.hidden = true;
}
start().catch(error => {
  console.error('Model viewer initialization failed',error);
  status.textContent = '模型未能显示。请使用支持 WebGL 2 的浏览器打开，并启用图形加速。';
});
