/** @module UseLatestScroll @description Initializes a scroll viewport at its latest content without pinning later user navigation. */
import {useLayoutEffect,type RefObject} from "react";

export function useLatestScroll(ref:RefObject<HTMLElement|null>,resetKey:string){
  useLayoutEffect(()=>{
    const viewport=ref.current;
    if(viewport)viewport.scrollTop=viewport.scrollHeight;
  },[ref,resetKey]);
}

export function scrollToCheckpoint(id:string){
  document.getElementById(id)?.scrollIntoView({behavior:"smooth",block:"start"});
}
