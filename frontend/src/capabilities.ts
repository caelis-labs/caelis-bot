export type FeatureCapability = {
 supported: boolean;
 available: boolean;
 enabled: boolean;
 permission?: string;
};
export type FeatureCapabilities = {
 capture: FeatureCapability;
 paste: FeatureCapability;
};
