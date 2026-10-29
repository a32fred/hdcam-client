#include <assert.h>
#include <errno.h>
#include <stdlib.h>
int *__errno(void) { return &errno; }
#include "local_only.c"

int main(void) {
    unsigned char local[16] = {2, 0, 0, 0, 192, 168, 56, 2};
    unsigned char external[16] = {2, 0, 0, 0, 8, 8, 8, 8};
    unsigned char other_lan[16] = {2, 0, 0, 0, 192, 168, 57, 2};
    setenv("HDCAM_ALLOWED_SUBNET", "192.168.56.0/24", 1); configure();
    assert(allowed(local, 16));
    assert(!allowed(other_lan, 16));
    assert(!allowed(external, 16));
    assert(!allowed(local, 2));
    errno = 0; assert(sendto(-1, "x", 1, 0, external, 16) == -1 && errno == 13);
    errno = 0; assert(connect(-1, external, 16) == -1 && errno == 13);
    struct socket_message message = {0}; message.name = external; message.name_length = 16;
    errno = 0; assert(sendmsg(-1, &message, 0) == -1 && errno == 13);
    struct socket_messages messages[2] = {0};
    messages[0].message.name = local; messages[0].message.name_length = 16;
    messages[1].message.name = external; messages[1].message.name_length = 16;
    errno = 0; assert(sendmmsg(-1, messages, 2, 0) == -1 && errno == 13);
    setenv("HDCAM_ALLOWED_SUBNET", "0.0.0.0/0", 1); configure(); assert(!allowed(local, 16));
    setenv("HDCAM_ALLOWED_SUBNET", "8.8.8.0/24", 1); configure(); assert(!allowed(external, 16));
    setenv("HDCAM_ALLOWED_SUBNET", "invalid", 1); configure(); assert(!allowed(local, 16));
    return 0;
}
