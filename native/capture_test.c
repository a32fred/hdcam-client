#define CAPTURE_TEST
#define write test_write
#include "capture.c"
#undef write

static unsigned char output[1024];
static usize output_size;
static int writes, mode, error_number;
int *__errno(void) { return &error_number; }
ssize test_write(int fd, const void *data, usize length) {
    (void)fd;
    writes++;
    if (mode == 1 && writes == 1) { error_number = 4; return -1; }
    if (mode == 2) return 0;
    if (mode == 3) { error_number = 5; return -1; }
    usize count = length > 3 ? 3 : length;
    if (output_size + count > sizeof(output)) return -1;
    memcpy(output + output_size, data, count);
    output_size += count;
    return (ssize)count;
}
static void put32(unsigned char *p, u32 n) {
    p[0] = n; p[1] = n >> 8; p[2] = n >> 16; p[3] = n >> 24;
}
int main(void) {
    unsigned char frame[37] = {0}, storage[96];
    put32(frame, 0xa815aa55); frame[4] = 3; put32(frame + 16, 5);
    frame[32] = 1; frame[33] = 2; frame[34] = 3; frame[35] = 4; frame[36] = 5;
    struct assembler a = {storage, 0, 32, 64, 3};
    mode = 1;
    for (usize i = 0; i < sizeof(frame); i++) {
        if (assemble(&a, frame + i, 1)) return 1;
        if (i + 1 < sizeof(frame) && output_size) return 2;
    }
    if (output_size != sizeof(frame) || writes < 2 || a.used) return 3;
    for (usize i = 0; i < sizeof(frame); i++) if (output[i] != frame[i]) return 4;
    if (assemble(&a, frame, 34) || a.used != 34 || output_size != 37) return 5;
    a.used = 0; a.target = 32;
    frame[4] = 11;
    if (!assemble(&a, frame, 32)) return 6;
    a.used = 0; a.target = 32; frame[4] = 3; put32(frame + 16, 65);
    if (!assemble(&a, frame, 32)) return 7;
    a.used = 0; a.target = 32; put32(frame + 16, 0);
    if (!assemble(&a, frame, 32)) return 8;
    a.used = 0; a.target = 32; put32(frame + 16, 5); frame[0] = 0;
    if (!assemble(&a, frame, 32)) return 9;
    put32(frame, 0xa815aa55);
    for (mode = 2; mode <= 3; mode++) {
        a.used = 0; a.target = 32;
        if (!assemble(&a, frame, sizeof(frame)) || output_lock) return 10;
    }
    frame[4] = 11; a.type = 11; a.used = 0; a.target = 32; mode = 0;
    if (assemble(&a, frame, sizeof(frame))) return 11;
    unsigned char pair[74];
    memcpy(pair, frame, 37); memcpy(pair + 37, frame, 37);
    output_size = 0;
    if (assemble(&a, pair, sizeof(pair)) || output_size != 74 || a.used) return 12;
    a.max_payload = 2048; put32(frame + 16, 2049);
    if (!assemble(&a, frame, 32)) return 13;
    return 0;
}
