use hex;
use sha2::{Digest, Sha256};
use url::Url;

/// Size based on:
/// https://stackoverflow.com/questions/417142/what-is-the-maximum-length-of-a-url-in-different-browsers
const URL_BUFFER_SIZE: usize = 2048;

/// Buffer that null-terminated URL's and the result SHA-256 hashes are written into.
static mut URL_BUFFER: [u8; URL_BUFFER_SIZE] = [0; URL_BUFFER_SIZE];

#[unsafe(no_mangle)]
pub fn get_url_ptr() -> *const u8 {
    unsafe {
        #[allow(static_mut_refs)]
        URL_BUFFER.as_ptr()
    }
}

pub fn read_buffer() -> Result<String, std::string::FromUtf8Error> {
    #[allow(static_mut_refs)]
    let url_block = unsafe {
        URL_BUFFER
            .iter()
            .take_while(|b| **b != 0)
            .cloned()
            .collect()
    };
    String::from_utf8(url_block)
}

pub fn write_buffer(s: &str) {
    // Return hex-encoded hash adding the terminating null-byte to the end.
    for (i, b) in s.bytes().chain(std::iter::once(0)).enumerate() {
        unsafe {
            URL_BUFFER[i] = b;
        }
    }
}

/// Normalize the URL written in the buffer.
///
/// The normalization will:
/// - Remove dot segments e.g., http://host/path/./a/b/../c -> http://host/path/a/c
/// - Sort query parameters by key e.g., http://host/path?c=3&b=2&a=1&b=1 -> http://host/path?a=1&b=1&b=2&c=3
/// - TODO Do all things that the purell library FlagsSafe does, see: https://github.com/PuerkitoBio/purell
pub fn static_normalize_url() -> Result<String, i32> {
    let Ok(input) = read_buffer() else {
        return Err(1);
    };

    let Ok(mut url) = Url::parse(&input) else {
        return Err(2);
    };

    let normalized_url = {
        {
            let mut sorted_query = Vec::new();
            for (k, v) in url.query_pairs() {
                sorted_query.push((k.into_owned(), v.into_owned()));
            }
            sorted_query.sort();
            url.set_query(None);
            for (k, v) in sorted_query {
                url.query_pairs_mut().append_pair(&k, &v);
            }
        }
        url.to_string()
    };

    return Ok(normalized_url);
}

pub fn static_hash_url(url: String) {
    // Compute SHA-256 hash.
    let mut hasher = Sha256::new();
    hasher.update(url.as_bytes());
    let hash = hasher.finalize();

    write_buffer(&hex::encode(hash));
}

#[unsafe(no_mangle)]
pub fn static_normalize_and_hash_url() -> i32 {
    match static_normalize_url() {
        Ok(url) => {
            static_hash_url(url);
            return 0;
        }
        Err(err_code) => return err_code,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn get_normalized(url: &str) -> String {
        write_buffer(url);

        let normalized = static_normalize_url().unwrap();
        return normalized;
    }

    fn get_hash(url: &str) -> String {
        write_buffer(url);

        let result = static_normalize_and_hash_url();
        assert_eq!(result, 0);

        let hash = read_buffer().unwrap();
        return hash;
    }

    #[test]
    fn test_url_remove_dot_segments() {
        let url = get_normalized("http://host/path/./a/b/../c");
        assert_eq!(url, "http://host/path/a/c");
    }

    #[test]
    fn test_url_sort_query_params() {
        let url = get_normalized("http://host/path?c=3&b=2&a=1&b=1");
        assert_eq!(url, "http://host/path?a=1&b=1&b=2&c=3");
    }

    #[test]
    fn test_already_normalized_url() {
        let hash = get_hash("https://example.com/");
        assert_eq!(
            hash,
            "0f115db062b7c0dd030b16878c99dea5c354b49dc37b38eb8846179c7783e9d7"
        );
    }

    #[test]
    fn test_url_with_path() {
        let hash = get_hash("https://www.iltalehti.fi/telkku");
        assert_eq!(
            hash,
            "459be7edc490987a93c52288bf98d28485b9be7e47295b2ce083a1f89b36e0ec"
        );
    }
}
