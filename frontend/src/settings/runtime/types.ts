import type { ExecutionSettings, ModelOption, RuntimeSettings, SetupState, WorkExecutionSettings } from '../../backend/contract';

// Renderer presentation models. These are not a replacement for the generated
// Go contract or Caelis wire types. Native services own protocol projection.
export type ModelSelection = WorkExecutionSettings;
export type ModelScope = 'conversation' | 'runtime' | 'work';
export type TeamRole = { id: string; modelIds?: string[]; description: string; system: boolean; custom: boolean; selection: ModelSelection; inherited: boolean; problem?: string };
export type TeamSet = { name: string; available: boolean; problem?: string };
export type TeamState = { available: boolean; reason: string; revision: string; roles: TeamRole[]; sets: TeamSet[]; activeSet: string; models: ModelOption[] };
export type ConnectionModel = { id: string; name: string; uses: string[]; unavailable: boolean };
export type ConnectionGroup = { id: string; name: string; kind: 'provider' | 'agent'; detail: string; models: ConnectionModel[] };
export type RuntimeView = {
 revision: string; profile: RuntimeSettings; setup: SetupState; pending: string;
 models: ModelOption[]; conversation: ExecutionSettings | null; work: ModelSelection | null;
 main: ModelSelection | null; canEditMain: boolean; team: TeamState; connections: ConnectionGroup[];
};
export type ConnectionKind = 'account' | 'api-key' | 'agent';
export type ConnectChoice = { id: string; name: string; description: string; custom?: boolean };
export type ConnectionCatalog = { choices: ConnectChoice[]; unavailable: string };
export type APIKeyOptions = { endpoints: { value: string; name: string; reuseAuth: boolean }[]; models: { value: string; name: string }[] };
export type AuthMethod = { id: string; name: string; description: string; available: boolean; reason?: string };
export type ConnectionStage = 'preparing' | 'launcher' | 'installation' | 'auth-method' | 'authorization' | 'models' | 'complete' | 'failed' | 'unknown';
export type ConnectionFlow = {
 id: string; revision: string; sequence: number; stage: ConnectionStage; title: string; message: string;
 installation?: { destination: string; source: string; platform: string; instructions: string; canInstall: boolean };
 launchers?: ConnectChoice[]; methods?: AuthMethod[];
 authorization?: { url: string; userCode?: string; inputLabel?: string; expiresAt?: string; canSubmit: boolean };
 models?: { id: string; name: string; description: string }[];
};
export type ConnectInput = { kind: ConnectionKind; choice: string; command?: string; baseUrl?: string; model?: string; apiKey?: string; contextWindowTokens?: number; maxOutputTokens?: number; imageInput?: boolean; reasoningLevels?: string[] };
export type ConnectAction = 'choose-launcher' | 'install' | 'check-installation' | 'authenticate' | 'submit-code' | 'connect' | 'refresh';
export type FlowInput = { destination?: string; launcher?: string; method?: string; code?: string; model?: string };
export type TeamChange =
 | { action: 'bind'; id: string; selection: ModelSelection }
 | { action: 'reset' | 'delete-role'; id: string }
 | { action: 'create-role'; id: string; description: string }
 | { action: 'save-set' | 'apply-set' | 'delete-set'; name: string };

// A frontend seam for the user-owned settings surface, never a model tool.
// No implementation may infer an auth URL, installation command or capability.
export interface RuntimeSettingsClient {
 read(): Promise<RuntimeView>;
 saveModel(scope: ModelScope, value: ModelSelection, revision?: string): Promise<void>;
 changeTeam(change: TeamChange, revision: string): Promise<void>;
 removeModel(group: ConnectionGroup, model: ConnectionModel, revision?: string): Promise<void>;
 catalog(kind: ConnectionKind): Promise<ConnectionCatalog>;
 apiKeyOptions(provider: string, baseUrl: string): Promise<APIKeyOptions>;
 startConnection(input: ConnectInput, signal: AbortSignal, progress: (flow: ConnectionFlow) => void): Promise<ConnectionFlow>;
 advanceConnection(flow: ConnectionFlow, action: ConnectAction, input: FlowInput, signal: AbortSignal, progress: (flow: ConnectionFlow) => void): Promise<ConnectionFlow>;
 cancelConnection(flow: ConnectionFlow): Promise<void>;
 openURL(url: string): Promise<void>;
}
