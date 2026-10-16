"""Run the selector inside a terminal owned by a persistent parent session."""
import base64
import errno
import fcntl
import json
import os
import pty
import select
import signal
import struct
import subprocess
import sys
import termios
import time
from pathlib import Path


def reset_child_interrupt():
    signal.signal(signal.SIGINT, signal.SIG_DFL)


if sys.argv[1] == '--terminal-host':
    os.setsid()
    fcntl.ioctl(0, termios.TIOCSCTTY, 0)
    os.tcsetpgrp(0, os.getpgrp())
    # Like an interactive parent shell, keep the terminal alive after Ctrl+C
    # and after the selected command exits so its restored state can be read.
    signal.signal(signal.SIGINT, signal.SIG_IGN)
    original = termios.tcgetattr(0)
    child = subprocess.Popen(['bash', sys.argv[2]], preexec_fn=reset_child_interrupt)
    status = child.wait()
    final = termios.tcgetattr(0)
    # macOS sets PENDIN when restoring canonical mode. It is kernel bookkeeping
    # for the next read, rather than a configurable echo/input setting.
    original[3] &= ~getattr(termios, 'PENDIN', 0)
    final[3] &= ~getattr(termios, 'PENDIN', 0)
    Path(sys.argv[3]).write_text(json.dumps({
        'exit_code': status,
        'terminal_restored': original == final,
        'terminal_delta': repr(original) + ' -> ' + repr(final),
    }))
    sys.exit(0)

master, slave = pty.openpty()
process = None
receipt = Path(sys.argv[1] + '.terminal.json')
try:
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack('HHHH', 24, 100, 0, 0))
    process = subprocess.Popen(
        [sys.executable, __file__, '--terminal-host', sys.argv[1], str(receipt)],
        stdin=slave, stdout=slave, stderr=slave,
    )
    output = bytearray()
    sent = False
    deadline = time.monotonic() + 10
    while True:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise TimeoutError('selector did not finish')
        ready, _, _ = select.select([master], [], [], min(remaining, 0.2))
        if ready:
            try:
                chunk = os.read(master, 65536)
            except OSError as error:
                if error.errno != errno.EIO:
                    raise
                break
            if not chunk:
                break
            output.extend(chunk)
            if not sent and b'q: cancel' in output:
                sent = True
                keys = b'\x03' if sys.argv[3] == 'interrupt' else base64.b64decode(sys.argv[2])
                os.write(master, keys)
        elif process.poll() is not None:
            break
    process.wait(timeout=1)
    result = json.loads(receipt.read_text())
    result.update(keys_sent=sent, output=output.decode('utf-8', errors='replace'))
    print(json.dumps(result))
finally:
    if process is not None and process.poll() is None:
        os.killpg(process.pid, signal.SIGKILL)
        process.wait()
    receipt.unlink(missing_ok=True)
    os.close(master)
    os.close(slave)
