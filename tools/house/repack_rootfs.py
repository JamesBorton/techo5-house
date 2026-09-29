#!/usr/bin/env python3
# usage: repack_rootfs.py <official rootfs.tar.gz> <our echod-arm> <out.tar.gz>
# Official TECHO5 rootfs with only usr/local/bin/techo5 replaced by our build.
import tarfile, io, os, sys, time
src, binp, out = sys.argv[1:4]
data = open(binp, 'rb').read()
with tarfile.open(src, 'r:gz') as tin, tarfile.open(out, 'w:gz', format=tarfile.GNU_FORMAT) as tout:
    replaced = False
    for m in tin:
        if m.name in ('./usr/local/bin/techo5', 'usr/local/bin/techo5') and m.isfile():
            m.size = len(data); m.mtime = int(time.time())
            tout.addfile(m, io.BytesIO(data)); replaced = True
        else:
            tout.addfile(m, tin.extractfile(m) if m.isfile() else None)
print('replaced' if replaced else 'NOT FOUND', os.path.getsize(out))
