#include <vector>
#include <numeric>
extern "C" int vector_sum(int n) { std::vector<int> v(n); std::iota(v.begin(), v.end(), 1); return std::accumulate(v.begin(), v.end(), 0); }
