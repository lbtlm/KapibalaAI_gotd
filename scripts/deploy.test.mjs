import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp,writeFile,rm} from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import {createHash} from 'node:crypto';
import {verifyCandidate} from './deploy.mjs';

test('candidate verification binds the exact commit and refuses tampered images before Docker', async () => {
  const dir = await mkdtemp(path.join(os.tmpdir(),'kapibala-candidate-'));
  try {
    const files = {'COMMIT_SHA':'a'.repeat(40)+'\n','images.tar.gz':'fake archive','compose.yaml':'services: {}\n'};
    for(const [name,body] of Object.entries(files)) await writeFile(path.join(dir,name),body);
    await writeFile(path.join(dir,'SHA256SUMS'),Object.entries(files).map(([name,body])=>createHash('sha256').update(body).digest('hex')+'  '+name).join('\n')+'\n');
    assert.equal(await verifyCandidate(dir),'a'.repeat(40));
    await writeFile(path.join(dir,'images.tar.gz'),'changed archive');
    await assert.rejects(verifyCandidate(dir),/checksum mismatch/);
  } finally {await rm(dir,{recursive:true,force:true});}
});
