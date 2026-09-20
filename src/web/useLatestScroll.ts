/** @module UseLatestScroll @description Initializes a scroll viewport at its latest content without pinning later user navigation. */
import {useLayoutEffect,type RefObject} from "react";

export function useLatestScroll(ref:RefObject<HTMLElement|null>,resetKey:string){
  useLayoutEffect(()=>{
    const viewport=ref.current;
    if(viewport)viewport.scrollTop=viewport.scrollHeight;
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
