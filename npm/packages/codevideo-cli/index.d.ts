export interface CodeVideoCliRuntimeOptions {
  platform?: NodeJS.Platform;
  arch?: string;
  env?: NodeJS.ProcessEnv;
  binaryPath?: string;
  runnerPath?: string;
}

export interface CodeVideoCliRuntime {
  binaryPath: string;
  runnerPath: string;
  workDir: string;
  logDir: string;
  outputDir: string;
  env: NodeJS.ProcessEnv;
}

export declare function getCodeVideoCliPlatformPackage(platform?: NodeJS.Platform, arch?: string): string;
export declare function resolveCodeVideoCliRuntime(options?: CodeVideoCliRuntimeOptions): CodeVideoCliRuntime;
