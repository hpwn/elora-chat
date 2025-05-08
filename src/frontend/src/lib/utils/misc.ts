import { deployedUrl, useDeployedApi } from '$lib/config';

export function buildApiUrl(path: string): string {
  return `${useDeployedApi ? deployedUrl : ''}${path}`;
}
