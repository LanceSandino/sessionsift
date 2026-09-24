/** @module UseMediaQuery @description Tracks a CSS media query so layout-dependent controls report the state the reader actually sees. */
import {useEffect,useState} from "react";

export function matchesMedia(query:string):boolean{
  return typeof window!=="undefined"&&typeof window.matchMedia==="function"&&window.matchMedia(query).matches;
}

export function useMediaQuery(query:string):boolean{
  const[matches,setMatches]=useState(()=>matchesMedia(query));
  useEffect(()=>{
    if(typeof window.matchMedia!=="function")return;
    const list=window.matchMedia(query);
    const update=()=>setMatches(window.matchMedia(query).matches);
    update();
    // Some engines and viewport emulators skip MediaQueryList change events; a resize always re-checks.
    list.addEventListener("change",update);
    window.addEventListener("resize",update);
    return()=>{list.removeEventListener("change",update);window.removeEventListener("resize",update)};
  },[query]);
  return matches;
}
