/** @module Sources @description Provider metadata for Codex, Claude Code, ChatGPT, and Claude conversations. */
import type {Session} from "./types";

export type SourceId="codex"|"claude_code"|"chatgpt"|"claude";

export type SourceInfo={id:SourceId;label:string;icon:string;assistant:string;chat:boolean};

export const sources:readonly SourceInfo[]=[
  {id:"codex",label:"Codex",icon:"◈",assistant:"Codex",chat:false},
  {id:"claude_code",label:"Claude Code",icon:"✳",assistant:"Claude",chat:false},
  {id:"chatgpt",label:"ChatGPT",icon:"✦",assistant:"ChatGPT",chat:true},
  {id:"claude",label:"Claude",icon:"✺",assistant:"Claude",chat:true},
];

/** Sessions recorded before provider tagging are Codex transcripts. */
export function sourceOf(session:Pick<Session,"source">|undefined):SourceId{
  const value=session?.source;
  return sources.some(source=>source.id===value)?value as SourceId:"codex";
}

export function sourceInfo(session:Pick<Session,"source">|undefined):SourceInfo{
  const id=sourceOf(session);
  return sources.find(source=>source.id===id) as SourceInfo;
}

/** Chats show their product; developer sessions show their repository when known. */
export function sessionPlace(session:Pick<Session,"source"|"repo_display_name">):string{
  const info=sourceInfo(session);
  return info.chat?info.label:session.repo_display_name||info.label;
}

export function sourceCounts(sessions:Pick<Session,"source"|"session_kind">[]):Record<SourceId,number>{
  const counts:Record<SourceId,number>={codex:0,claude_code:0,chatgpt:0,claude:0};
  for(const session of sessions){
    if(session.session_kind==="internal_review")continue;
    counts[sourceOf(session)]+=1;
  }
  return counts;
}
