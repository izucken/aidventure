# Simple deploy

Project MUST build entirely into one binary that is runnable portably.
Any configs or data is ALWAYS rooted to the binary location.
Project NEVER reaches into any other place on fs and keeps everything it needs near the binary.
Project MUST be able to ship as a single binary file.
Portability is very important - no OS dependencies must be required.
