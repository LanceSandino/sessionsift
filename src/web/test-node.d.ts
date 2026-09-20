/** @module TestNodeTypes @description Declares the narrow Node file API used by source-contract tests. */
declare module "node:fs" {
  export function readFileSync(path:string|URL,encoding:"utf8"):string;
}
