/** @module CommandPalette @description Ctrl/Cmd K search across sessions, views, workspace pages and actions, fully keyboard operable. */
import React,{useEffect,useMemo,useRef,useState} from "react";
import {Icon,type IconName} from "./Icon";
import {useNativeModal} from "./Dialog";

export type Command={id:string;label:string;icon:IconName;kind:"session"|"view"|"page"|"action";run:()=>void};

const sessionLimit=30;

/** Case-insensitive match on every word; with no query, every non-session command plus the latest sessions. */
export function matchCommands(commands:Command[],query:string):Command[]{
  const words=query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  const matched=words.length?commands.filter(command=>words.every(word=>command.label.toLowerCase().includes(word))):commands;
  const sessions=matched.filter(command=>command.kind==="session").slice(0,words.length?sessionLimit:6);
  return [...sessions,...matched.filter(command=>command.kind!=="session")];
}

export function CommandPalette({open,commands,onClose}:{open:boolean;commands:Command[];onClose:()=>void}){
  const ref=useRef<HTMLDialogElement>(null);
  const input=useRef<HTMLInputElement>(null);
  const[query,setQuery]=useState("");
  const[active,setActive]=useState(0);
  const handlers=useNativeModal(ref,open,onClose);
  const results=useMemo(()=>matchCommands(commands,query),[commands,query]);
  useEffect(()=>{if(open){setQuery("");setActive(0);requestAnimationFrame(()=>input.current?.focus())}},[open]);
  useEffect(()=>setActive(0),[query]);
  useEffect(()=>{ref.current?.querySelector(`[data-index="${active}"]`)?.scrollIntoView?.({block:"nearest"})},[active]);
  const run=(command:Command|undefined)=>{if(!command)return;onClose();command.run()};
  const onKey=(event:React.KeyboardEvent)=>{
    if(event.key==="ArrowDown"||event.key==="ArrowUp"){event.preventDefault();if(results.length)setActive(value=>(value+(event.key==="ArrowDown"?1:-1)+results.length)%results.length)}
    if(event.key==="Enter"){event.preventDefault();run(results[active])}
  };
  return <dialog ref={ref} id="command-dialog" aria-label="Command palette" {...handlers}>
    {open&&<>
      <input ref={input} className="command-input" role="combobox" aria-expanded="true" aria-controls="command-results" aria-activedescendant={results[active]?`command-${active}`:undefined} aria-label="Search commands and sessions" placeholder="Find a session, view or action…" autoComplete="off" value={query} onChange={event=>setQuery(event.target.value)} onKeyDown={onKey}/>
      <div className="command-results" id="command-results" role="listbox" aria-label="Results">
        {results.map((command,index)=><button type="button" key={command.id} id={`command-${index}`} data-index={index} role="option" aria-selected={index===active} className={`command-result ${index===active?"active":""}`} tabIndex={-1} onMouseEnter={()=>setActive(index)} onClick={()=>run(command)}><Icon name={command.icon}/><span>{command.label}</span><small>{command.kind}</small></button>)}
        {!results.length&&<div className="empty">No matching session or command.</div>}
      </div>
      <div className="command-footer"><span><kbd>↑</kbd> <kbd>↓</kbd> navigate</span><span><kbd>Enter</kbd> open</span><span><kbd>Esc</kbd> close</span></div>
    </>}
  </dialog>;
}
