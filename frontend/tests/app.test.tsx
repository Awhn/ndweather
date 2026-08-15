import React from 'react';
import {act,cleanup,fireEvent,render,screen} from '@testing-library/react';
import '@testing-library/jest-dom/vitest';
import {afterEach,describe,expect,it,vi} from 'vitest';
import {App,Dashboard} from '../src/main';

const data:Dashboard={sites:[{code:'SAMPLE',name:'샘플',warningAreaCodes:[]}],observations:[{site_code:'SAMPLE',temperature:20}],forecasts:[],warnings:[],radarFrames:[],typhoons:[],status:{state:'normal',rotateSeconds:5,refreshSeconds:30,radarFrameSeconds:1}};

afterEach(()=>{cleanup();vi.useRealTimers();vi.unstubAllGlobals();window.history.replaceState({},'', '/display')});

describe('display',()=>{
  it('renders pages without the regional map and supports keyboard control',()=>{
    vi.useFakeTimers();
    render(<App initial={data}/>);
    expect(screen.getByText('샘플')).toBeInTheDocument();
    expect(screen.queryByLabelText('대한민국 지역 배치도')).not.toBeInTheDocument();
    act(()=>vi.advanceTimersByTime(5000));
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
