typedef unsigned long usize;
typedef long ssize;
extern void *dlsym(void *, const char *);
extern int *__errno(void);
extern char *getenv(const char *);
extern unsigned long strlen(const char *);
extern void *memcpy(void *, const void *, usize);
extern char *strchr(const char *, int);
extern unsigned long strtoul(const char *, char **, int);
extern int inet_pton(int, const char *, void *);
static unsigned int network, mask;
static int configured;
static int allowed(const void *address, unsigned int length);

static unsigned int address_value(const unsigned char *a) {
    return (unsigned int)a[0] << 24 | (unsigned int)a[1] << 16 | (unsigned int)a[2] << 8 | a[3];
}

__attribute__((constructor)) static void configure(void) {
    configured = 0;
    const char *value = getenv("HDCAM_ALLOWED_SUBNET");
    if (!value || strlen(value) >= 64) return;
    char text[64]; memcpy(text, value, strlen(value) + 1);
    char *slash = strchr(text, '/'); if (!slash) return; *slash = 0;
    char *end; unsigned long bits = strtoul(slash + 1, &end, 10);
    unsigned char ip[4];
    if (end == slash + 1 || *end || bits > 32 || inet_pton(2, text, ip) != 1) return;
    if (!((ip[0] == 10 && bits >= 8) || (ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31 && bits >= 12) ||
          (ip[0] == 192 && ip[1] == 168 && bits >= 16))) return;
    mask = ~0u << (32 - bits); network = address_value(ip) & mask; configured = 1;
}

struct socket_message {
    void *name;
    unsigned int name_length;
    void *vectors;
    usize vector_count;
    void *control;
    usize control_length;
    int flags;
};
struct socket_messages { struct socket_message message; unsigned int length; };

ssize sendmsg(int fd, const struct socket_message *message, int flags) {
    if (!message || !allowed(message->name, message->name_length)) { *__errno() = 13; return -1; }
    ssize (*real)(int, const struct socket_message *, int) = dlsym((void *)-1, "sendmsg");
    if (!real) { *__errno() = 13; return -1; }
    return real(fd, message, flags);
}

int sendmmsg(int fd, struct socket_messages *messages, unsigned int count, int flags) {
    if ((!messages && count) || count > 1024) { *__errno() = 13; return -1; }
    for (unsigned int i = 0; i < count; ++i) {
        if (!allowed(messages[i].message.name, messages[i].message.name_length)) { *__errno() = 13; return -1; }
    }
    int (*real)(int, struct socket_messages *, unsigned int, int) = dlsym((void *)-1, "sendmmsg");
    if (!real) { *__errno() = 13; return -1; }
    return real(fd, messages, count, flags);
}

static int allowed(const void *address, unsigned int length) {
    if (!address) return 1;
    if (length < 2) return 0;
    const unsigned char *a = address;
    unsigned short family = *(const unsigned short *)a;
    if (family == 1) return 1;
    if (family != 2 || length < 16 || !configured) return 0;
    unsigned int ip = address_value(a + 4);
    return ip >> 24 == 127 || ip == ~0u || (ip & mask) == network;
}

int connect(int fd, const void *address, unsigned int length) {
    if (!allowed(address, length)) { *__errno() = 13; return -1; }
    int (*real)(int, const void *, unsigned int) = dlsym((void *)-1, "connect");
    if (!real) { *__errno() = 13; return -1; }
    return real(fd, address, length);
}

ssize sendto(int fd, const void *data, usize size, int flags, const void *address, unsigned int length) {
    if (!allowed(address, length)) { *__errno() = 13; return -1; }
    ssize (*real)(int, const void *, usize, int, const void *, unsigned int) = dlsym((void *)-1, "sendto");
    if (!real) { *__errno() = 13; return -1; }
    return real(fd, data, size, flags, address, length);
}

static int numeric(const char *name) {
    if (!name) return 1;
    for (const char *p = name; *p; ++p) if ((*p < '0' || *p > '9') && *p != '.') return 0;
    return 1;
}

int getaddrinfo(const char *name, const char *service, const void *hints, void *result) {
    if (!numeric(name)) return 8;
    int (*real)(const char *, const char *, const void *, void *) = dlsym((void *)-1, "getaddrinfo");
    if (!real) return 8;
    return real(name, service, hints, result);
}

void *gethostbyname(const char *name) {
    if (!name || !numeric(name)) return (void *)0;
    void *(*real)(const char *) = dlsym((void *)-1, "gethostbyname");
    return real ? real(name) : (void *)0;
}
