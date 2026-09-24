/** @module HostedPagesTest @description Syntax-checks every inline script the Go bridge serves, since no bundler sees them. */
import {readFileSync} from "node:fs";
import {describe,expect,it} from "vitest";

const source=readFileSync("src/internal/bridge/web.go","utf8");

function pageScripts(name:string):string[]{
  const start=source.indexOf(`const ${name} = \``);
  expect(start,`page ${name} exists`).toBeGreaterThanOrEqual(0);
  const body=source.slice(start,source.indexOf("</html>`",start));
  return [...body.matchAll(/<script>([\s\S]*?)<\/script>/g)].map(match=>match[1]);
}

describe("hosted bridge pages",()=>{
  for(const page of ["connectPage","invitePage","supportPage"]){
    it(`${page} inline script parses`,()=>{
      const scripts=pageScripts(page);
      expect(scripts.length).toBe(1);
      // Compile only; nothing executes. A syntax error here blanks the page in production.
      expect(()=>new Function(scripts[0])).not.toThrow();
    });
  }
});
