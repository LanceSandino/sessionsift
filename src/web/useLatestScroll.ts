/** @module UseLatestScroll @description Opens a scroll viewport at its latest content, or where the reader left it, without pinning later user navigation. */
import {useLayoutEffect,type RefObject} from "react";

export type ScrollOptions={
  /** Remembered positions by reset key; a revisited view reopens where the reader left it. */
  memory?:Map<string,number>;
  /** Where a view the reader has not visited opens. */
  start?:"latest"|"top";
};

export function useLatestScroll(ref:RefObject<HTMLElement|null>,resetKey:string,options:ScrollOptions={}){
  const {memory,start="latest"}=options;
  useLayoutEffect(()=>{
    const viewport=ref.current;
    if(!viewport)return;
    const saved=memory?.get(resetKey);
    let pinned=saved===undefined&&start==="latest";
    let width=viewport.clientWidth,height=viewport.clientHeight;
    viewport.scrollTop=saved!==undefined?saved:pinned?viewport.scrollHeight:0;
    const onScroll=()=>{
      memory?.set(resetKey,viewport.scrollTop);
      // A breakpoint or hidden pane is not a user's decision to leave latest.
      if(viewport.clientWidth!==width||viewport.clientHeight!==height||!height)return;
      pinned=viewport.scrollHeight-viewport.clientHeight-viewport.scrollTop<4;
    };
    const observer=typeof ResizeObserver==="undefined"?undefined:new ResizeObserver(()=>{
      width=viewport.clientWidth;height=viewport.clientHeight;
      if(pinned&&height)viewport.scrollTop=viewport.scrollHeight;
    });
    observer?.observe(viewport);
    if(viewport.firstElementChild)observer?.observe(viewport.firstElementChild);
    viewport.addEventListener("scroll",onScroll,{passive:true});
    return()=>{observer?.disconnect();viewport.removeEventListener("scroll",onScroll)};
  },[ref,resetKey,memory,start]);
}

export function scrollToCheckpoint(id:string,viewport?:HTMLElement|null){
  const target=document.getElementById(id);
  if(!target)return;
  if(viewport&&viewport.contains(target)){
    const top=viewport.scrollTop+target.getBoundingClientRect().top-viewport.getBoundingClientRect().top;
    if(typeof viewport.scrollTo==="function")viewport.scrollTo({top,behavior:"smooth"});
    else viewport.scrollTop=top;
    return;
  }
  target.scrollIntoView({behavior:"smooth",block:"start"});
}
