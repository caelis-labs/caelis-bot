# Capture explicit evidence

Load [Observation](desktop-observation.md) and authorize the app. Use
`bot_desktop_inspect` with `request.type:"image"` only when metadata cannot locate
or verify the relevant UI. The model must support images. No action or read
captures automatically; recovery returns metadata only, never new pixels.

For window content, first outline the application scope with
`projection:"capture_windows"` and fields `["name","role"]`. This directory is
metadata, not pixels. Choose its observed `role:"capture_window"` Ref, then:

```json
{"request":{"type":"image","kind":"window_content","target":"OBSERVED_CAPTURE_REF","max_pixel_width":1000,"max_pixel_height":1000}}
```

Capture Refs are not AX window Refs; never match solely by title or use them for
focus, children or input. `window_content` captures the full target, without
`region` or `include_cursor:true`. Hidden/minimized windows can be refused;
sheets/popups can have separate Refs. Its `image_to_target` transform is local
to the captured window; empty `desktop_frame` and zero `image_to_desktop` grant
no desktop coordinate mapping or input authority. Preserve the exact transform.

Use a small `visible_region` when visible desktop placement is actually needed.
It may include occluding applications, which need grants too. A window's bounds
are not proof of unobscured pixels. Preserve its returned image-to-desktop
transform; never treat window-content pixels as visible-region coordinates.
Request one tile and at most 1000 pixels per dimension. Multiple tiles require
a narrower request. Capture permission/capability failures have no automatic
fallback. Pixels and AX facts are separate samples, not simultaneous proof.
