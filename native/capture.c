/* Android ARM64 freestanding bridge. SDK binaries and initialization are external. */
typedef unsigned int u32;
typedef unsigned long usize;
typedef long ssize;
extern void *dlopen(const char *, int);
extern void *dlsym(void *, const char *);
extern char *getenv(const char *);
extern int *__errno(void);
extern ssize read(int, void *, usize);
extern ssize write(int, const void *, usize);
extern usize strlen(const char *);
extern void *memcpy(void *, const void *, usize);
extern int dprintf(int, const char *, ...);
extern void exit(int);
struct timespec { long seconds; long nanoseconds; };
extern int clock_gettime(int, struct timespec *);
extern void (*signal(int, void (*)(int)))(int);
static volatile int stopped;
static void stop(int value) { (void)value; stopped = 1; }

static int read_all(void *buffer, usize length) {
    char *p = buffer;
    while (length) {
        ssize n = read(0, p, length);
        if (n < 0 && *__errno() == 4) continue;
        if (n <= 0) return -1;
        p += n; length -= (usize)n;
    }
    return 0;
}

static int write_all(const void *buffer, usize length) {
    const char *p = buffer;
    while (length) {
        ssize n = write(1, p, length);
        if (n < 0 && *__errno() == 4) continue;
        if (n <= 0) return -1;
        p += n; length -= (usize)n;
    }
    return 0;
}

static u32 le32(const unsigned char *p) {
    return (u32)p[0] | (u32)p[1] << 8 | (u32)p[2] << 16 | (u32)p[3] << 24;
}

void _start(void) {
    unsigned char header[20];
    char sdk_init[4097], uid[129], command[65537];
    if (read_all(header, sizeof(header))) exit(2);
    u32 init_size = le32(header + 4), uid_size = le32(header + 8);
    u32 command_size = le32(header + 12), seconds = le32(header + 16);
    if (le32(header) != 0x31434448 || !init_size || init_size > 4096 ||
        !uid_size || uid_size > 128 || !command_size || command_size > 65536 || !seconds || seconds > 3600) exit(2);
    if (read_all(sdk_init, init_size) || read_all(uid, uid_size) || read_all(command, command_size)) exit(2);
    sdk_init[init_size] = uid[uid_size] = command[command_size] = 0;
    if (strlen(sdk_init) != init_size || strlen(uid) != uid_size || strlen(command) != command_size) exit(2);
    const char *library = getenv("HDCAM_LIBRARY");
    if (!library) exit(2);
    void *sdk = dlopen(library, 2);
    if (!sdk) { dprintf(2, "SDK load failed\n"); exit(3); }
    int (*initialize)(const char *) = dlsym(sdk, "HLP2P_Initialize");
    int (*connect)(const char *, char, unsigned short) = dlsym(sdk, "HLP2P_Connect");
    int (*send)(int, unsigned char, const char *, int) = dlsym(sdk, "HLP2P_Write");
    int (*receive)(int, unsigned char, char *, int *, unsigned int) = dlsym(sdk, "HLP2P_Read");
    int (*close_session)(int) = dlsym(sdk, "HLP2P_Close");
    if (!initialize || !connect || !send || !receive || !close_session) exit(3);
    int status = initialize(sdk_init);
    if (status < 0) { dprintf(2, "SDK initialization failed: %d\n", status); exit(3); }
    int session = connect(uid, 0, 0);
    if (session < 0) { dprintf(2, "SDK connection failed: %d\n", session); exit(3); }
    char request[65544];
    u32 request_header[2] = {0x80a00a06, command_size};
    memcpy(request, request_header, 8); memcpy(request + 8, command, command_size);
    if (send(session, 0, request, (int)command_size + 8) != (int)command_size + 8) { close_session(session); exit(4); }
    signal(2, stop); signal(15, stop); signal(13, (void (*)(int))1);
    struct timespec now;
    if (clock_gettime(1, &now)) { close_session(session); exit(4); }
    long deadline = now.seconds + seconds;
    long deadline_nanoseconds = now.nanoseconds;
    char buffer[1048576];
    int failed = 0;
    while (!stopped) {
        if (clock_gettime(1, &now)) { failed = 1; break; }
        if (now.seconds > deadline || (now.seconds == deadline && now.nanoseconds >= deadline_nanoseconds)) break;
        int size = sizeof(buffer);
        status = receive(session, 1, buffer, &size, 1000);
        if (size < 0 || (usize)size > sizeof(buffer) || (status < 0 && status != -3)) { failed = 1; break; }
        if (size && write_all(buffer, (usize)size)) { failed = 1; break; }
    }
    close_session(session);
    exit(failed || stopped ? 4 : 0);
}
