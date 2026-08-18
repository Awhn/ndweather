import React, {useCallback, useEffect, useMemo, useState} from 'react';
import {createRoot} from 'react-dom/client';
import './style.css';

type Site = {code:string; name:string; warningAreaCodes:string[]};
type Observation = {site_code:string; observed_at?:string; temperature?:number|null; humidity?:number|null; wind_direction?:string; wind_speed?:number|null; gust_speed?:number|null; precipitation?:number|null; precipitation_state?:string; sky?:string};
type Forecast = {site_code:string; issued_at:string; valid_at:string; min_temperature?:number|null; max_temperature?:number|null; rain_probability?:number|null; sky?:string};
type Warning = {id:number; warning_id:string; phenomenon:string; level:string; area_code:string; area_name:string; announced_at:string; site_codes?:string[]};
type RadarFrame = {asset_id:string; observed_at:string; url:string};
type ForecastPoint = {forecast_at:string; latitude:number; longitude:number};
type Typhoon = {typhoon_key:string; number:string; name:string; latitude:number; longitude:number; pressure?:number|null; max_wind?:number|null; direction?:string; speed?:number|null; forecastPoints?:ForecastPoint[]};
type DashboardStatus = {state:'normal'|'stale'|'disconnected'; lastSuccessfulReceiveAt?:string; demo?:boolean; rotateSeconds:number; refreshSeconds:number; radarFrameSeconds:number};
export type Dashboard = {sites:Site[]; observations:Observation[]; forecasts:Forecast[]; warnings:Warning[]; radarFrames:RadarFrame[]; typhoons:Typhoon[]; status:DashboardStatus};

const empty:Dashboard={sites:[],observations:[],forecasts:[],warnings:[],radarFrames:[],typhoons:[],status:{state:'disconnected',rotateSeconds:5,refreshSeconds:30,radarFrameSeconds:1}};
const koreaTime = new Intl.DateTimeFormat('ko-KR',{timeZone:'Asia/Seoul',dateStyle:'medium',timeStyle:'medium'});
const koreaDay = new Intl.DateTimeFormat('en-CA',{timeZone:'Asia/Seoul',year:'numeric',month:'2-digit',day:'2-digit'});
const formatTime=(value?:string)=>value ? koreaTime.format(new Date(value)) : '없음';
const dayKey=(value:string|Date)=>koreaDay.format(typeof value==='string'?new Date(value):value);
const isDashboard=(value:unknown):value is Dashboard=>{
  if(!value||typeof value!=='object')return false;
  const x=value as Partial<Dashboard>;
  return ['sites','observations','forecasts','warnings','radarFrames','typhoons'].every(key=>Array.isArray(x[key as keyof Dashboard]))&&!!x.status&&typeof x.status==='object';
};

function RadarImage({item,token,onError}:{item:RadarFrame;token:string;onError:()=>void}){
  const[src,setSrc]=useState('');
  useEffect(()=>{
    let active=true,objectURL='';
    fetch(item.url,{headers:token?{Authorization:`Bearer ${token}`}:{}}).then(response=>{
      if(!response.ok)throw new Error(`radar HTTP ${response.status}`);
      return response.blob();
    }).then(blob=>{
      if(!active)return;
      objectURL=URL.createObjectURL(blob);
      setSrc(objectURL);
    }).catch(()=>{if(active)onError()});
    return()=>{active=false;if(objectURL)URL.revokeObjectURL(objectURL)};
  },[item.url,token,onError]);
  return src?<img src={src} alt="레이더 영상"/>:<div className="placeholder">레이더 영상을 불러오는 중입니다</div>;
}

export function App({initial}:{initial?:Dashboard}){
  const query=useMemo(()=>new URLSearchParams(location.search),[]);
  const requestedPage=Number(query.get('page'));
  const fixedPage=requestedPage===1||requestedPage===2?requestedPage:null;
  const readToken=useMemo(()=>new URLSearchParams(location.hash.replace(/^#/,'')).get('token')||'',[]);
  const[data,setData]=useState(initial||empty);
  const[page,setPage]=useState(fixedPage||1);
  const[paused,setPaused]=useState(query.get('rotate')==='false'||fixedPage!==null);
  const[clock,setClock]=useState(new Date());
  const[frame,setFrame]=useState(0);
  const[broken,setBroken]=useState(false);
  const[loadError,setLoadError]=useState('');

  useEffect(()=>{
    if(initial)return;
    let active=true;
    const load=async()=>{
      try{
        const response=await fetch('/api/v1/display/dashboard',{headers:readToken?{Authorization:`Bearer ${readToken}`}:{}});
        if(!response.ok)throw new Error(response.status===401?'READ_API_TOKEN이 필요합니다':`HTTP ${response.status}`);
        const payload=await response.json();
        if(!isDashboard(payload.data))throw new Error('대시보드 응답 형식이 올바르지 않습니다');
        if(active){setData(payload.data);setLoadError('')}
      }catch(error){if(active)setLoadError(error instanceof Error?error.message:'대시보드 수신 실패')}
    };
    load();
    const id=setInterval(load,(data.status.refreshSeconds||30)*1000);
    return()=>{active=false;clearInterval(id)};
  },[initial,data.status.refreshSeconds,readToken]);
  useEffect(()=>{const id=setInterval(()=>setClock(new Date()),1000);return()=>clearInterval(id)},[]);
  useEffect(()=>{if(paused)return;const id=setInterval(()=>setPage(value=>value===1?2:1),(data.status.rotateSeconds||5)*1000);return()=>clearInterval(id)},[paused,data.status.rotateSeconds]);
  useEffect(()=>{const key=(event:KeyboardEvent)=>{if(event.code==='Space'){event.preventDefault();setPaused(value=>!value)}if(event.key==='ArrowLeft')setPage(1);if(event.key==='ArrowRight')setPage(2)};addEventListener('keydown',key);return()=>removeEventListener('keydown',key)},[]);
  useEffect(()=>{setFrame(value=>data.radarFrames.length?Math.min(value,data.radarFrames.length-1):0);setBroken(false)},[data.radarFrames]);
  useEffect(()=>{if(paused||!data.radarFrames.length)return;const id=setInterval(()=>{setFrame(value=>(value+1)%data.radarFrames.length);setBroken(false)},(data.status.radarFrameSeconds||1)*1000);return()=>clearInterval(id)},[paused,data.radarFrames.length,data.status.radarFrameSeconds]);

  const observations=useMemo(()=>Object.fromEntries(data.observations.map(item=>[item.site_code,item])),[data.observations]);
  const today=dayKey(clock);
  const forecastFor=(siteCode:string)=>data.forecasts.find(item=>item.site_code===siteCode&&dayKey(item.valid_at)===today&&(item.min_temperature!=null||item.max_temperature!=null))||data.forecasts.find(item=>item.site_code===siteCode);
  const state=loadError?'disconnected':data.status.state||'disconnected';
  const label=state==='normal'?'정상':state==='stale'?'자료 지연':'연계중단';
  const hasWeather=data.observations.length>0||data.forecasts.length>0;
  const radar=data.radarFrames[frame];
  const radarError=useCallback(()=>setBroken(true),[]);

  return <main>
    <header><div><b>KOEN</b><h1>사업소 기상정보 통합상황판</h1>{data.status.demo&&<strong className="demo">DEMO DATA</strong>}</div><div className="clock">{koreaTime.format(clock)}<span className={'state '+state}>● {label}</span></div></header>
    <section className="viewport" aria-live="polite">
      {page===1?<div className="page page1">
        {!data.sites.length||!hasWeather?<Empty last={data.status.lastSuccessfulReceiveAt} error={loadError}/>:<div className="cards">{data.sites.map(site=>{const observation=observations[site.code]||{};const forecast=forecastFor(site.code);return <article className="card" key={site.code}><div className="title"><h2>{site.name}</h2><code>{site.code}</code></div><div className="temp">{observation.temperature??'--'}<small>°C</small></div><dl><div><dt>습도</dt><dd>{observation.humidity??'--'}%</dd></div><div><dt>바람</dt><dd>{observation.wind_direction||'--'} {observation.wind_speed??'--'}m/s</dd></div><div><dt>강수</dt><dd>{observation.precipitation_state||'--'} {observation.precipitation??'--'}mm</dd></div><div><dt>하늘</dt><dd>{observation.sky||forecast?.sky||'--'}</dd></div><div><dt>오늘 최저/최고</dt><dd>{forecast?.min_temperature??'--'} / {forecast?.max_temperature??'--'}°</dd></div></dl><time>기준 {formatTime(observation.observed_at||forecast?.valid_at)}</time></article>})}</div>}
      </div>:<div className="page page2"><div className="radar"><h2>레이더 합성영상 <small>{formatTime(radar?.observed_at)}</small></h2>{radar&&!broken?<RadarImage item={radar} token={readToken} onError={radarError}/>:<div className="placeholder">레이더 이미지가 없거나 손상되었습니다</div>}</div><div className="right"><article><h2>발효 중 주요 기상특보</h2>{data.warnings.length?data.warnings.map(item=><div className="alert" key={item.warning_id}><strong>{item.level} · {item.phenomenon}</strong><span>{item.area_name}</span><time>발표 {formatTime(item.announced_at)}</time></div>):<p>현재 발효 중인 주요 특보 없음</p>}</article><article><h2>태풍·열대저압부</h2>{data.typhoons.length?data.typhoons.map(item=><div key={item.typhoon_key} className="typhoon"><strong>제{item.number}호 {item.name}</strong><p>중심 {item.latitude}°N, {item.longitude}°E · {item.pressure??'--'}hPa</p><p>최대풍속 {item.max_wind??'--'}m/s · {item.direction||'--'} {item.speed??'--'}km/h</p><p>예상 경로 {item.forecastPoints?.length||0}개 지점</p></div>):<p>현재 발표된 태풍 정보 없음</p>}</article></div></div>}
    </section>
    <footer><span>자료 발표·관측시각 {formatTime(data.observations.map(item=>item.observed_at||'').sort().at(-1))}</span><span>내부 수신시각 {formatTime(data.status.lastSuccessfulReceiveAt)}</span><span className="controls"><button type="button" onClick={()=>setPage(value=>value===1?2:1)}>PAGE {page}/2</button><button type="button" aria-pressed={paused} onClick={()=>setPaused(value=>!value)}>{paused?'전환 재개':'전환 정지'}</button></span></footer>
  </main>
}
function Empty({last,error}:{last?:string;error?:string}){return <div className="empty"><strong>{error||'수신된 기상자료가 없습니다'}</strong><span>마지막 정상 수신시각: {formatTime(last)}</span></div>}
const root=document.getElementById('root');if(root)createRoot(root).render(<App/>);
