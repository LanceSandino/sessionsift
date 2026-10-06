/** @module ActivityCallouts @description Derives evidence-qualified protocol, Blueprint, memory and file-read labels from recorded tool metadata. */
import type {ToolCall} from "./types";

export type ActivityCalloutKind="protocol"|"blueprint"|"memory"|"file";
export type ActivityCallout={kind:ActivityCalloutKind;label:string;protocol?:string};
export type ActivitySummary={protocols:string[];blueprintReads:number;memoryReads:number;fileReads:number};

const protocolNames=["CAP","GCP","MSP","PPP","TICP","CDP","ASAP","SHRP"] as const;

/** Only recorded read calls qualify; an acronym in prose or a search result is not protocol-use evidence. */
export function activityCallouts(call:ToolCall):ActivityCallout[]{
  if(call.tool_category!=="read")return [];
  const evidence=[call.tool_name,call.target_path_display,call.arguments_redacted,call.command_signature].filter(Boolean).join("\n");
  const protocols=protocolNames.filter(name=>new RegExp(`(?:^|[^a-z0-9])${name}(?:\\.instructions)?\\.md(?:$|[^a-z0-9])`,"i").test(evidence));
  const result:ActivityCallout[]=protocols.map(protocol=>({kind:"protocol",protocol,label:`${protocol} protocol read`}));
  if(/(?:^|[^a-z0-9])blueprint(?:[-_][^/\\\s"']+)?\.md(?:$|[^a-z0-9])/i.test(evidence))result.push({kind:"blueprint",label:"Blueprint read"});
  if(/(?:^|[^a-z0-9])memory(?:_summary)?\.md(?:$|[^a-z0-9])|(?:^|[/\\])memories[/\\]/i.test(evidence))result.push({kind:"memory",label:"Memory read"});
  if(!result.length)result.push({kind:"file",label:"File read"});
  return result;
}

export function summarizeActivity(calls:ToolCall[]):ActivitySummary{
  const protocols=new Set<string>();
  let blueprintReads=0;
  let memoryReads=0;
  let fileReads=0;
  for(const call of calls){
    if(call.tool_category!=="read")continue;
    fileReads++;
    const callouts=activityCallouts(call);
    if(callouts.some(item=>item.kind==="blueprint"))blueprintReads++;
    if(callouts.some(item=>item.kind==="memory"))memoryReads++;
    for(const item of callouts)if(item.protocol)protocols.add(item.protocol);
  }
  return {protocols:protocolNames.filter(name=>protocols.has(name)),blueprintReads,memoryReads,fileReads};
}
