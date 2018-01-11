# pgtest
Start a local postgres for testing in your go tests.

## FAQ

### Mac 

##### FATAL:  could not create shared memory segment: No space left on device


Please add 

```bash
kern.sysv.shmall=786432
kern.sysv.shmmax=3221225472 #  kern.sysv.shmall * 4096
kern.sysv.shmmni=4096
```

to  /etc/sysctl.conf and reboot.
