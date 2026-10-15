import {readFile,mkdir,writeFile} from 'node:fs/promises';
import path from 'node:path';
import {createHash} from 'node:crypto';
import {spawnSync} from 'node:child_process';
import {pathToFileURL,fileURLToPath} from 'node:url';

export async function verifyCandidate(directory) {
  const sums = await readFile(path.join(directory,'SHA256SUMS'),'utf8');
  const expected = new Map();
  for (const line of sums.trim().split('\n')) {
    const match = /^([a-f0-9]{64})  (COMMIT_SHA|compose.yaml|images.tar.gz)$/.exec(line);
    if (!match || expected.has(match[2])) throw new Error('invalid candidate checksum manifest');
    expected.set(match[2],match[1]);
  }
  for (const name of ['COMMIT_SHA','compose.yaml','images.tar.gz']) {
    const digest = createHash('sha256').update(await readFile(path.join(directory,name))).digest('hex');
    if (digest !== expected.get(name)) throw new Error(`checksum mismatch: ${name}`);
  }
  const sha = (await readFile(path.join(directory,'COMMIT_SHA'),'utf8')).trim();
  if (!/^[a-f0-9]{40}$/.test(sha)) throw new Error('candidate commit must be an immutable 40-character SHA');
  return sha;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    const [directoryArg,project,mode='deploy'] = process.argv.slice(2);
    if (!directoryArg || !/^[a-z0-9][a-z0-9_-]*$/.test(project || '') || !['deploy','rollback'].includes(mode)) {
      throw new Error('Usage: node scripts/deploy.mjs <accepted-candidate-directory> <dedicated-compose-project> [deploy|rollback]');
    }
    if (!process.env.POSTGRES_PASSWORD || !/^[a-zA-Z0-9_-]+$/.test(process.env.POSTGRES_PASSWORD)) {
      throw new Error('POSTGRES_PASSWORD must be set to a URI-safe password in this process');
    }
    const directory = path.resolve(directoryArg);
    const sha = await verifyCandidate(directory);
    const env = {...process.env,CANDIDATE_SHA:sha,COMPOSE_PROJECT_NAME:project};
    const docker = (args) => {
      const result = spawnSync(process.env.DOCKER_COMMAND || 'docker',args,{env,stdio:'inherit',windowsHide:true});
      if (result.error || result.status !== 0) throw new Error('Docker operation failed; candidate was not accepted as healthy');
    };
    docker(['load','--input',path.join(directory,'images.tar.gz')]);
    // Refuse missing original images before stopping a working application.
    docker(['image','inspect','--format','{{.Id}}',...['backend','gateway','frontend'].map(component=>`kapibala-${component}:${sha}`)]);
    const compose = ['compose','-f',path.join(directory,'compose.yaml'),'-p',project];
    if (mode === 'deploy') {
      docker([...compose,'stop','backend','agent','gateway']);
      docker([...compose,'up','-d','--no-build','postgres']);
      docker([...compose,'run','--rm','--pull','never','migrate']);
    }
    docker([...compose,'up','-d','--no-build','--no-deps','postgres','gateway','agent','backend','frontend']);
    const url = `http://127.0.0.1:${env.CONSOLE_PORT || '5173'}/api/health`;
    let healthy = false;
    for (let i=0; i<60; i++) {
      try {if ((await fetch(url,{signal:AbortSignal.timeout(1000)})).ok) {healthy=true;break;}} catch {}
      await new Promise(resolve=>setTimeout(resolve,1000));
    }
    if (!healthy) throw new Error('Candidate health check failed; investigate logs and use an accepted compatible rollback candidate');
    const stateDirectory = fileURLToPath(new URL('../.local/deploy/',import.meta.url));
    await mkdir(stateDirectory,{recursive:true});
    await writeFile(path.join(stateDirectory,project+'.json'),JSON.stringify({sha,mode,verifiedAt:new Date().toISOString()})+'\n');
    console.log(`Local candidate ${sha} is healthy at ${url}; functional acceptance remains separate.`);
  } catch(error) {console.error(error.message);process.exitCode=1;}
}
