use std::collections::HashMap;

fn main() {
    // Parse command line arguments.
    let args: HashMap<String, String> = std::env::args()
        .skip(1)
        .fold(
            (Option::<&str>::None, HashMap::new()),
            |(prev, mut acc), x| {
                if prev.is_some() {
                    acc.insert(prev.unwrap().to_string(), x);
                    return (None, acc);
                }
                for key in ["url"] {
                    if x.starts_with(&format!("--{}", key)) {
                        return (Some(key), acc);
                    }
                }
                for flag in ["sign"] {
                    if x.starts_with(&format!("--{}", flag)) {
                        acc.insert(flag.to_string(), "true".to_string());
                        return (None, acc);
                    }
                }
                eprintln!("Unknown argument: {}", x);
                std::process::exit(1);
            },
        )
        .1;

    println!("URL: {}", args["url"]);
    println!("Hashing the url: {}", args.contains_key("sign"));

    suora::write_buffer(&args["url"]);

    let normalized_url = match suora::static_normalize_url() {
        Ok(url) => url,
        Err(err) => {
            eprintln!("Error: {}", err);
            std::process::exit(1);
        }
    };

    if args.contains_key("sign") {
        suora::static_hash_url(normalized_url);
        match suora::read_buffer() {
            Err(err) => {
                eprintln!("Error: {}", err);
                std::process::exit(1);
            }
            Ok(hash) => {
                println!("{}", hash);
            }
        }
    } else {
        println!("{}", normalized_url);
    }
}
