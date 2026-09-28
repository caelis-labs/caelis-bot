# Computer Use third-party components

Caelis Bot's own source is Apache-2.0. The following bundled components retain
their own licenses. This notice does not relicense their covered source files.

- Node.js 24.21.0: MIT and included third-party terms; see the bundled
  `Node-LICENSE`. Matching source: https://nodejs.org/dist/v24.21.0/node-v24.21.0.tar.gz
- `@trycua/cua-driver` 0.30.2: MIT. Copyright (c) 2025 Cua AI, Inc.
  Native platform package: MIT AND MPL-2.0. Release source:
  https://github.com/trycua/cua/tree/a2229c5b829153ec3b1828387bc72ca8f1f18704
- `@ubjs/core`, `@ubjs/node` and its `@ubjs/node-darwin-*` platform package 0.31.0-3: MPL-2.0. The matching npm package source
  and notices are retained in `node_modules/@ubjs`. Upstream source:
  https://github.com/jhugman/uniffi-bindgen-react-native
- UniFFI 0.31.0: MPL-2.0. Matching source:
  https://github.com/mozilla/uniffi-rs/tree/v0.31.0

MPL-covered source remains available under MPL-2.0; see `MPL-2.0.txt`.
The native platform package's `node-runtime-NOTICE.md` identifies the N-API
compatibility runtime as derived from `uniffi-bindgen-react-native` 0.31.0-3.
Its corresponding source is that exact npm version together with the deterministic
transformations in `libs/cua-driver/scripts/build-node-runtime.mjs` in the
matching Cua release. Obtain the pinned package through:
https://registry.npmjs.org/uniffi-bindgen-react-native/-/uniffi-bindgen-react-native-0.31.0-3.tgz

The Cua and UniFFI source URLs provide the native SDK sources as well. The optional Cua perception extension, ONNX Runtime and model weights are not
part of this payload. No local source
modifications are made to these upstream components. Preserve upstream notices
and source availability when redistributing this payload.

MIT license (Cua):

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
