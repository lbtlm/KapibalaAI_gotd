import {spawn} from 'node:child_process';
import {createInterface} from 'node:readline';
import path from 'node:path';
import {fileURLToPath,pathToFileURL} from 'node:url';

export function allowedSkip(module,event,env=process.env,compiledOnly=new Set()) {
  if (!event.Test && compiledOnly.has(event.Package)) return true;
  if (module === 'gateway' && event.Package === 'github.com/lbtlm/KapibalaAI_gotd/gateway' && event.Test === 'TestGatewayCrashChild') {
    return env.GATEWAY_CRASH_CHILD !== '1';
  }
  if (module !== 'backend') return false;
  if (event.Package === 'github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform' && event.Test === 'TestServerProcessHelper') {
    return env.KAPIBALA_CHILD_SERVER !== 'true';
  }
  return event.Package === 'github.com/lbtlm/KapibalaAI_gotd/backend/internal/provider' && event.Test === 'TestLiveProviderToolConversationAndAudit' && env.RUN_LIVE_PROVIDER !== 'true';
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
const root = fileURLToPath(new URL('../', import.meta.url));
const module = process.argv[2];
const required = {backend:'TEST_DATABASE_URL', gateway:'GATEWAY_TEST_DATABASE_URL'}[module];
if (!required || !process.env[required]) {
  console.error(`${required || 'Go module'} is required: CI integration tests may not silently skip`);
  process.exit(1);
}
const args = ['test', '-json', '-count=1', ...process.argv.slice(3), './...'];
const child = spawn('go', args, {cwd:path.join(root,module), stdio:['ignore','pipe','inherit'], windowsHide:true});
let unexpectedSkip = false;
const compiledOnly = new Set();
createInterface({input:child.stdout}).on('line', line => {
  try {
    const event = JSON.parse(line);
    if (event.Output) process.stdout.write(event.Output);
    if (event.Action === 'output' && !event.Test) {
      const packageName = /^\?\s+(\S+)\s+\[no test files\]$/.exec(event.Output?.trim() || '')?.[1];
      if (packageName === event.Package) compiledOnly.add(packageName);
    }
    if (event.Action === 'skip' && !allowedSkip(module,event,process.env,compiledOnly)) {
      unexpectedSkip = true;
      console.error(`Unexpected skipped test: ${event.Package} ${event.Test || ''}`);
    }
  } catch { console.log(line); }
});
child.on('error', error => {console.error(error.message); process.exitCode=1;});
child.on('close', code => {process.exitCode=code === 0 && !unexpectedSkip ? 0 : 1;});
}
