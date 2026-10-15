/** @module ReleaseSBOM emits a deterministic SPDX 2.3 JSON inventory from locked Go and npm dependencies. */
import {execFileSync} from "node:child_process";
import {readFileSync,writeFileSync} from "node:fs";
import {resolve} from "node:path";

const root=resolve(new URL("..",import.meta.url).pathname);
const output=process.argv[2];
if(!output) throw new Error("usage: node scripts/generate-sbom.mjs <output.json>");
const version=JSON.parse(readFileSync(resolve(root,"package.json"),"utf8")).version;
const commit=execFileSync("git",["-C",root,"rev-parse","HEAD"],{encoding:"utf8"}).trim();
const created=new Date(Number(execFileSync("git",["-C",root,"show","-s","--format=%ct",commit],{encoding:"utf8"}).trim())*1000).toISOString();
const goOutput=execFileSync("go",["list","-m","-f","{{.Path}}\t{{.Version}}","all"],{cwd:root,encoding:"utf8"});
const goModules=goOutput.trim().split("\n").map(line=>{const [Path,Version]=line.split("\t");return{Path,Version}}).filter(value=>value.Path&&value.Version);
const lock=JSON.parse(readFileSync(resolve(root,"package-lock.json"),"utf8"));
const dependencies=[];
for(const module of goModules) dependencies.push({name:module.Path,versionInfo:module.Version,downloadLocation:`https://proxy.golang.org/${module.Path}/@v/${module.Version}.zip`,supplier:"NOASSERTION"});
for(const [path,value] of Object.entries(lock.packages||{})) {
  if(!path||!value.version) continue;
  const name=value.name||path.split("node_modules/").pop();
  if(name) dependencies.push({name,versionInfo:value.version,downloadLocation:value.resolved||"NOASSERTION",supplier:"NOASSERTION"});
}
dependencies.sort((a,b)=>`${a.name}@${a.versionInfo}`.localeCompare(`${b.name}@${b.versionInfo}`));
const packages=[{SPDXID:"SPDXRef-Package-SessionSift",name:"sessionsift",versionInfo:version,downloadLocation:"NOASSERTION",filesAnalyzed:false,licenseConcluded:"PolyForm-Noncommercial-1.0.0",licenseDeclared:"PolyForm-Noncommercial-1.0.0",copyrightText:"NOASSERTION"},...dependencies.map((value,index)=>({SPDXID:`SPDXRef-Dependency-${index+1}`,...value,filesAnalyzed:false,licenseConcluded:"NOASSERTION",licenseDeclared:"NOASSERTION",copyrightText:"NOASSERTION"}))];
const relationships=[{spdxElementId:"SPDXRef-DOCUMENT",relationshipType:"DESCRIBES",relatedSpdxElement:"SPDXRef-Package-SessionSift"},...packages.slice(1).map(value=>({spdxElementId:"SPDXRef-Package-SessionSift",relationshipType:"DEPENDS_ON",relatedSpdxElement:value.SPDXID}))];
const document={spdxVersion:"SPDX-2.3",dataLicense:"CC0-1.0",SPDXID:"SPDXRef-DOCUMENT",name:`sessionsift-${version}`,documentNamespace:`https://sessionsift.local/spdx/${version}/${commit}`,creationInfo:{created,creators:["Tool: SessionSift release SBOM generator"]},packages,relationships};
writeFileSync(output,`${JSON.stringify(document,null,2)}\n`,{mode:0o644});
