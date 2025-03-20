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

pub fn static_normalize_url() -> Result<String, i32> {
    let Ok(input) = read_buffer() else {
        return Err(1);
    };

    let Ok(url) = Url::parse(&input) else {
        return Err(2);
    };

    let normalized_url = {
        let mut x = url.to_owned();
        // Remove fragment as it doesn't affect resource identity
        x.set_fragment(None);
        // (Optional) You can also remove query parameters if needed
        x.set_query(None);
        x.to_string()
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

    fn get_hash(url: &str) -> String {
        unsafe {
            URL_BUFFER = [0; URL_BUFFER_SIZE];
        }

        for (i, b) in url.bytes().chain(std::iter::once(0)).enumerate() {
            unsafe {
                URL_BUFFER[i] = b;
            }
        }

        let result = static_normalize_and_hash_url();
        assert_eq!(result, 0);

        let hash = unsafe {
            #[allow(static_mut_refs)]
            let hash_block = URL_BUFFER
                .iter()
                .take_while(|b| **b != 0)
                .cloned()
                .collect();
            String::from_utf8(hash_block).unwrap()
        };

        return hash;
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
