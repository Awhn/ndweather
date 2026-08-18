import React from 'react';
import {act,cleanup,fireEvent,render,screen} from '@testing-library/react';
import '@testing-library/jest-dom/vitest';
import {afterEach,describe,expect,it,vi} from 'vitest';
import {App,Dashboard} from '../src/main';

const data:Dashboard={sites:[{code:'SAMPLE',name:'샘플',warningAreaCodes:['AREA']}],observations:[{site_code:'SAMPLE',temperature:20,gust_speed:12}],forecasts:[],warnings:[{id:1,warning_id:'WARN',phenomenon:'호우',level:'주의보',area_code:'AREA',area_name:'테스트 지역',announced_at:'2026-08-18T00:00:00Z',site_codes:['SAMPLE']}],radarFrames:[],typhoons:[],status:{state:'normal',rotateSeconds:5,refreshSeconds:30,radarFrameSeconds:1}};

afterEach(()=>{cleanup();vi.useRealTimers();vi.unstubAllGlobals();window.history.replaceState({},'', '/display')});

describe('display',()=>{
  it('hides page-one gusts and warnings while preserving page controls',()=>{
    vi.useFakeTimers();
    render(<App initial={data}/>);
    expect(screen.getByText('샘플')).toBeInTheDocument();
    expect(screen.queryByText('순간풍속')).not.toBeInTheDocument();
    expect(screen.queryByText(/호우/)).not.toBeInTheDocument();
    expect(screen.queryByLabelText('대한민국 지역 배치도')).not.toBeInTheDocument();
    act(()=>vi.advanceTimersByTime(5000));
    expect(screen.getByText('주의보 · 호우')).toBeInTheDocument();
    expect(screen.getByText('현재 발표된 태풍 정보 없음')).toBeInTheDocument();
    fireEvent.keyDown(window,{code:'Space'});
    fireEvent.keyDown(window,{key:'ArrowLeft'});
    expect(screen.getByText('샘플')).toBeInTheDocument();
  });

  it('shows no data state when sites exist but weather data does not',()=>{
    render(<App initial={{...data,observations:[],status:{...data.status,state:'stale'}}}/>);
    expect(screen.getByText('수신된 기상자료가 없습니다')).toBeInTheDocument();
    expect(screen.getByText(/자료 지연/)).toBeInTheDocument();
  });

  it('shows an authorization failure instead of keeping stale normal data',async()=>{
    vi.stubGlobal('fetch',vi.fn().mockResolvedValue({ok:false,status:401}));
    render(<App/>);
    expect(await screen.findByText('READ_API_TOKEN이 필요합니다')).toBeInTheDocument();
    expect(screen.getByText(/연계중단/)).toBeInTheDocument();
  });
});
