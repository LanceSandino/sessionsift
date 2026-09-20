/** @module UseLatestScroll @description Initializes a scroll viewport at its latest content without pinning later user navigation. */
import {useLayoutEffect,type RefObject} from "react";

export function useLatestScroll(ref:RefObject<HTMLElement|null>,resetKey:string){
  useLayoutEffect(()=>{
    const viewport=ref.current;
    if(!viewport)return;
    let pinned=true;
    let width=viewport.clientWidth,height=viewport.clientHeight;
    viewport.scrollTop=viewport.scrollHeight;
    const onScroll=()=>{
      // A breakpoint or hidden pane is not a user's decision to leave latest.
      if(viewport.clientWidth!==width||viewport.clientHeight!==height||!height)return;
      pinned=viewport.scrollHeight-viewport.clientHeight-viewport.scrollTop<4;
    };
    const observer=typeof ResizeObserver==="undefined"?undefined:new ResizeObserver(()=>{
      width=viewport.clientWidth;height=viewport.clientHeight;
      if(pinned&&height)viewport.scrollTop=viewport.scrollHeight;
    });
    observer?.observe(viewport);
    viewport.addEventListener("scroll",onScroll,{passive:true});
    return()=>{observer?.disconnect();viewport.removeEventListener("scroll",onScroll)};
  },[ref,resetKey]);
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
