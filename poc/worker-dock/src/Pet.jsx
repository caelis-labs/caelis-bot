import { useEffect, useRef, useState } from 'react';
import { Scene, OrthographicCamera, WebGLRenderer } from 'three';
import { loadCharacterModel } from '../../../frontend/src/character/load';
import { characterProfile } from '../../../frontend/src/character/profile';
import { CharacterAnimation } from '../../../frontend/src/character/animation';
import modelURL from '../../../frontend/public/models/caelis-soft-outfit-v1.glb?url';
import avatarURL from '../../../frontend/public/icons/caelis-avatar.png';

export function Pet({ gesture }) {
  const canvas = useRef(), player = useRef();
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let renderer;
    try { renderer = new WebGLRenderer({ canvas: canvas.current, alpha: true, antialias: true }); }
    catch { setFailed(true); return; }
    renderer.setPixelRatio(Math.min(devicePixelRatio, 2)); renderer.setSize(240, 320, false);
    const scene = new Scene(), camera = new OrthographicCamera(-1.05, 1.05, 2.42, -.38, .1, 20);
    camera.position.set(0, 0, 5);
    let disposed = false, frame, previous = performance.now(), profile, root;
    const reduced = matchMedia('(prefers-reduced-motion: reduce)');
    const draw = () => { renderer.shadowMap.needsUpdate = true; renderer.render(scene, camera); };
    const tick = now => {
      if (disposed || document.hidden) return;
      if (!reduced.matches) player.current?.update(Math.min((now - previous) / 1000, .05));
      previous = now; draw(); if (!reduced.matches) frame = requestAnimationFrame(tick);
    };
    const resume = () => { cancelAnimationFrame(frame); previous = performance.now(); if (!document.hidden && root) frame = requestAnimationFrame(tick); };
    const release = model => {
      const materials = new Set(), textures = new Set();
      model.traverse(o => { if (!o.isMesh) return; o.geometry.dispose(); for (const m of Array.isArray(o.material) ? o.material : [o.material]) materials.add(m); });
      for (const m of materials) { for (const value of Object.values(m)) if (value?.isTexture) textures.add(value); m.dispose(); }
      for (const t of textures) t.dispose();
    };
    loadCharacterModel(modelURL).then(gltf => {
      if (disposed) { release(gltf.scene); return; }
      root = gltf.scene; scene.add(root); profile = characterProfile(renderer, scene, root);
      player.current = new CharacterAnimation(root, gltf.animations); resume();
    }).catch(() => { if (!disposed) setFailed(true); });
    document.addEventListener('visibilitychange', resume); reduced.addEventListener('change', resume);
    return () => {
      disposed = true; cancelAnimationFrame(frame); document.removeEventListener('visibilitychange', resume); reduced.removeEventListener('change', resume);
      player.current?.dispose(); player.current = undefined; profile?.dispose(); if (root) release(root); renderer.dispose();
    };
  }, []);
  useEffect(() => { if (gesture && !matchMedia('(prefers-reduced-motion: reduce)').matches && !document.hidden) player.current?.gesture(gesture.name); }, [gesture]);
  return <div className="pet" aria-label="Caelis 角色，展开时轻抬头，选择时点头">{failed ? <img src={avatarURL} alt="Caelis 角色"/> : <canvas ref={canvas} aria-hidden="true"/>}</div>;
}
