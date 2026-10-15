// Runtime readiness gates optional choices and the first introduction.
export function setupStage(runtimeReady:boolean,featuresPending:boolean,permissionsPending:boolean) {
 return !runtimeReady?'runtime':featuresPending?'features':permissionsPending?'permissions':'identity';
}
