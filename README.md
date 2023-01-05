# pgtest
Start a local postgres for testing in your go tests.

## FAQ

### Mac 

##### FATAL:  could not create shared memory segment: No space left on device

Create the file /Library/LaunchDaemons/memory.plist
Add the content 

```xml
<plist version="1.0">
<dict>
 <key>Label</key>
 <string>shmemsetup</string>
 <key>UserName</key>
 <string>root</string>
 <key>GroupName</key>
 <string>wheel</string>
 <key>ProgramArguments</key>
 <array>
  <string>/usr/sbin/sysctl</string>
  <string>-w</string>
  <string>kern.sysv.shmmax=3221225472</string>
  <string>kern.sysv.shmmni=4096</string>
  <string>kern.sysv.shmseg=4096</string>
  <string>kern.sysv.shmall=33554432</string>
 </array>
 <key>KeepAlive</key>
 <false/>
 <key>RunAtLoad</key>
 <true/>
</dict>
</plist>
```

Then run
```bash
sudo chown root:wheel /Library/LaunchDaemons/memory.plist
sudo launchctl load -w /Library/LaunchDaemons/memory.plist
```

Then reboot
