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
extern void *malloc(usize);
extern void free(void *);
extern int sched_yield(void);
extern int pthread_create(unsigned long *, const void *, void *(*)(void *), void *);
extern int pthread_join(unsigned long, void **);
struct timespec { long seconds; long nanoseconds; };
extern int clock_gettime(int, struct timespec *);
extern void (*signal(int, void (*)(int)))(int);
static int output_lock;
#ifndef CAPTURE_TEST
/* Android ARM64 int atomics are lock-free, including from signal handlers. */
_Static_assert(__atomic_always_lock_free(sizeof(int), 0), "signal flag requires lock-free int atomics");
static int interrupted, stopped, failed;
static void stop(int value) { (void)value; __atomic_store_n(&interrupted, 1, __ATOMIC_RELAXED); }

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
#endif

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

struct assembler {
    unsigned char *bytes;
    usize used, target, max_payload;
    unsigned char type;
};

/* A channel retains its partial envelope until the complete payload arrives. */
#if defined(__aarch64__) && defined(__clang__)
/* Keep atomic lowering here: inlining can restore outlined runtime helpers. */
__attribute__((target("no-outline-atomics"), noinline))
#endif
static int assemble(struct assembler *a, const unsigned char *data, usize length) {
    while (length) {
        usize need = a->target - a->used;
        usize take = length < need ? length : need;
        memcpy(a->bytes + a->used, data, take);
        a->used += take; data += take; length -= take;
        if (a->used != a->target) continue;
        if (a->target == 32) {
            u32 payload = le32(a->bytes + 16);
            if (le32(a->bytes) != 0xa815aa55 || a->bytes[4] != a->type ||
                !payload || payload > a->max_payload) return -1;
            a->target = 32 + payload;
            continue;
        }
        while (__atomic_exchange_n(&output_lock, 1, __ATOMIC_ACQUIRE)) sched_yield();
        int result = write_all(a->bytes, a->target);
        __atomic_store_n(&output_lock, 0, __ATOMIC_RELEASE);
        if (result) return -1;
        a->used = 0; a->target = 32;
    }
    return 0;
}

#ifndef CAPTURE_TEST
struct channel {
    int session;
    unsigned char channel;
    long deadline, nanoseconds;
    int (*receive)(int, unsigned char, char *, int *, unsigned int);
};

static void *capture_channel(void *opaque) {
    struct channel *c = opaque;
    usize maximum = c->channel == 1 ? 4 * 1024 * 1024 : 2048;
    struct assembler a = {malloc(maximum + 32), 0, 32, maximum, c->channel == 1 ? 3 : 11};
    int receive_size = c->channel == 1 ? 4096 : 288;
    unsigned char *buffer = malloc(4096);
    if (!a.bytes || !buffer) goto error;
    while (!__atomic_load_n(&interrupted, __ATOMIC_RELAXED) && !__atomic_load_n(&stopped, __ATOMIC_ACQUIRE)) {
        struct timespec now;
        if (clock_gettime(1, &now)) goto error;
        if (now.seconds > c->deadline || (now.seconds == c->deadline && now.nanoseconds >= c->nanoseconds)) break;
        int size = receive_size;
        int status = c->receive(c->session, c->channel, (char *)buffer, &size, 100);
        if (size < 0 || size > receive_size || (status < 0 && status != -3)) goto error;
        if (size && assemble(&a, buffer, (usize)size)) goto error;
    }
    free(a.bytes); free(buffer);
    return 0;
error:
    __atomic_store_n(&failed, 1, __ATOMIC_RELEASE);
    __atomic_store_n(&stopped, 1, __ATOMIC_RELEASE);
    free(a.bytes); free(buffer);
    return 0;
}

void _start(void) {
    unsigned char header[20];
    char sdk_init[4097], uid[129], command[65537];
    if (read_all(header, sizeof(header))) exit(2);
    u32 init_size = le32(header + 4), uid_size = le32(header + 8);
    u32 command_size = le32(header + 12), seconds = le32(header + 16);
    int audio = le32(header) == 0x32434448;
    if ((!audio && le32(header) != 0x31434448) || !init_size || init_size > 4096 ||
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
    struct channel video = {session, 1, now.seconds + seconds, now.nanoseconds, receive};
    struct channel sound = {session, 2, video.deadline, video.nanoseconds, receive};
    unsigned long thread;
    if (audio && pthread_create(&thread, 0, capture_channel, &sound)) {
        close_session(session); exit(4);
    }
    capture_channel(&video);
    __atomic_store_n(&stopped, 1, __ATOMIC_RELEASE);
    if (audio && pthread_join(thread, 0)) { close_session(session); exit(4); }
    close_session(session);
    exit(__atomic_load_n(&failed, __ATOMIC_ACQUIRE) || __atomic_load_n(&interrupted, __ATOMIC_RELAXED) ? 4 : 0);
}
#endif
