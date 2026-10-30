const { test } = require('node:test');
const assert = require('node:assert/strict');
const http = require('node:http');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { render } = require('./renderFramesV1');

// No TTS/API calls: speech fixtures use prepared timing metadata. Actual audio
// crossing boundaries is covered by Go's TestDeterministicSpeechRender.
test('restored workers match uninterrupted browser frames at selected resumable boundaries', {skip:process.env.CODEVIDEO_BROWSER_TESTS !== '1',timeout:1200000}, async () => {
  const publicDir=path.resolve(__dirname,'../cli/staticserver/public');
  const mime={'.html':'text/html','.js':'application/javascript','.json':'application/json','.css':'text/css','.woff2':'font/woff2','.woff':'font/woff','.svg':'image/svg+xml','.png':'image/png'};
  const server=http.createServer((req,res)=>{
    let file=path.resolve(publicDir,'.'+decodeURIComponent(new URL(req.url,'http://localhost').pathname));
    if (!file.startsWith(publicDir+path.sep) && file!==publicDir) {res.writeHead(403).end();return;}
    if(fs.existsSync(file)&&fs.statSync(file).isDirectory())file=path.join(file,'index.html');
    if(!fs.existsSync(file)){res.writeHead(404).end();return;}
    res.setHeader('Content-Type',mime[path.extname(file)]||'application/octet-stream');fs.createReadStream(file).pipe(res);
  });
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const temp=process.env.CODEVIDEO_CHECKPOINT_TEST_OUTPUT || fs.mkdtempSync(path.join(os.tmpdir(),'codevideo-checkpoint-browser-'));
  fs.mkdirSync(temp,{recursive:true});
  const fixtureDir=path.join(__dirname,'fixtures');
  try {
    for(const file of (process.env.CODEVIDEO_CHECKPOINT_TEST_FIXTURES||'fixture-checkpoints.json,fixture-full.json').split(',')) {
      const project=JSON.parse(fs.readFileSync(path.join(fixtureDir,file))), actions=Array.isArray(project)?project:project.actions;
      const manifest={uuid:'checkpoint-test',renderSeed:'codevideo-golden-v1',...(Array.isArray(project)?{actions:project}:{lesson:project}),audioItems:actions.flatMap((a,actionIndex)=>a.name.startsWith('author-speak')?[{actionIndex,text:a.value,assetId:`speech-${actionIndex}`,durationMs:401}]:[])};
      if (process.env.CODEVIDEO_CHECKPOINT_TEST_HUMANIZED) manifest.codeVideoIDEProps = {includeTypos:true, keyboardTypingJitterMs:8};
      const name=path.basename(file,'.json'), results=[];
      for(const [strategy,workers] of [['sequential-seek',1],['ab-initio',Number(process.env.CODEVIDEO_CHECKPOINT_TEST_WORKERS || 8)]]) {
        const stem=path.join(temp,`${name}-${strategy}`);
        results.push(await render({manifest,strategy,workers,resolution:'1080p',orientation:'landscape',output:stem+'.mp4',trace:stem+'.trace.json',metrics:stem+'.metrics.json',baseUrl:`http://127.0.0.1:${server.address().port}`,hashAllFrames:true,sampleDir:process.env.CODEVIDEO_CHECKPOINT_SAVE_FRAMES ? fs.mkdirSync(stem+'-samples',{recursive:true}) || stem+'-samples' : undefined}));
      }
      assert.deepEqual(results[1].trace,results[0].trace,`${name}: trace`);
      assert.equal(results[1].metrics.evaluatedFrames,results[0].trace.frameCount);
      assert.ok(results[1].metrics.workers <= Math.min(Number(process.env.CODEVIDEO_CHECKPOINT_TEST_WORKERS || 8),results[1].metrics.eligibleCheckpoints+1));
      const expected=new Map(results[0].metrics.sampledFrames.map(f=>[f.frame,f.sha256]));
      const differences=results[1].metrics.sampledFrames.filter(f=>f.sha256!==expected.get(f.frame));
      assert.equal(differences.length,0,`${name}: differing frames ${differences.slice(0,30).map(f=>f.frame).join(',')}; artifacts ${temp}`);
      console.log(`Verified ${name}: ${expected.size} frames, ${results[1].metrics.workers} workers, selected resumable boundaries`);
    }
  } finally {await new Promise(resolve=>server.close(resolve));if(!process.env.CODEVIDEO_CHECKPOINT_TEST_OUTPUT)fs.rmSync(temp,{recursive:true,force:true,maxRetries:5});}
});
