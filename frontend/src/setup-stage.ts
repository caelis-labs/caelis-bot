// A confirmed model unlocks optional choices and editing the introduction.
// Actual activation is awaited separately before submitting the first message.
export function setupStage(runtimeSelected:boolean,featuresPending:boolean) {
 return !runtimeSelected?'runtime':featuresPending?'features':'identity';
}
